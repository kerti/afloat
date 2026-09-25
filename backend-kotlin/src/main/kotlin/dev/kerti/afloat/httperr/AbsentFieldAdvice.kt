package dev.kerti.afloat.httperr

import com.fasterxml.jackson.annotation.JsonProperty
import dev.kerti.afloat.api.model.LocalLoginRequest
import jakarta.validation.ConstraintViolation
import jakarta.validation.Validator
import org.springframework.core.MethodParameter
import org.springframework.http.HttpHeaders
import org.springframework.http.HttpInputMessage
import org.springframework.http.converter.HttpMessageConverter
import org.springframework.web.bind.annotation.ControllerAdvice
import org.springframework.web.servlet.mvc.method.annotation.RequestBodyAdviceAdapter
import tools.jackson.core.JacksonException
import tools.jackson.databind.JsonNode
import tools.jackson.databind.json.JsonMapper
import tools.jackson.databind.node.ObjectNode
import java.io.ByteArrayInputStream
import java.io.InputStream
import java.lang.reflect.Type
import kotlin.reflect.KClass
import kotlin.reflect.KParameter
import kotlin.reflect.full.findAnnotation
import kotlin.reflect.full.primaryConstructor

// An absent required field is 400 VALIDATION {field, rule: required} (the
// ruling on #18), and it competes with every other failing field for the one
// the envelope reports, least (field, rule) first (#13 §3.5) — as Go's
// spec-validating middleware does.
//
// The generated models make that impossible on their own: a required property
// is a non-null constructor parameter carrying @JsonProperty(required = true),
// so Jackson fails the decode on the first absent one and Bean Validation never
// sees the fields that ARE present. {"email":"not-an-email"} would say
// password/required where Go says email/email.
//
// So before Jackson reads a generated model, this looks for absent required
// fields. Finding some, it fills each with a placeholder, decodes and validates
// the result, drops the placeholders' own violations and reports the rest
// beside one `required` per absent field. If the filled body still does not
// decode — an unknown property, a null, a wrong type — the original goes on to
// Jackson and fails there as INVALID_JSON_BODY, which is Go's answer too: a
// body that is not the declared shape outranks any field constraint.
@ControllerAdvice
class AbsentFieldAdvice(
    private val mapper: JsonMapper,
    private val validator: Validator,
) : RequestBodyAdviceAdapter() {

    override fun supports(
        methodParameter: MethodParameter,
        targetType: Type,
        converterType: Class<out HttpMessageConverter<*>>,
    ): Boolean = (targetType as? Class<*>)?.packageName == GENERATED_MODELS

    // Buffering is bounded by MaxBodyFilter's 1 MiB; an over-long body throws
    // from readAllBytes and Spring answers it as unreadable, as before.
    override fun beforeBodyRead(
        inputMessage: HttpInputMessage,
        parameter: MethodParameter,
        targetType: Type,
        converterType: Class<out HttpMessageConverter<*>>,
    ): HttpInputMessage {
        val bytes = inputMessage.body.readAllBytes()
        absentFields(bytes, (targetType as Class<*>).kotlin)?.let { throw it }
        return Replayed(inputMessage.headers, bytes)
    }

    private fun absentFields(bytes: ByteArray, type: KClass<*>): AbsentFieldsException? {
        val body = try {
            mapper.readTree(bytes)
        } catch (_: JacksonException) {
            return null
        }
        if (body !is ObjectNode) return null
        val required = type.primaryConstructor?.parameters
            ?.filter { !it.isOptional && !it.type.isMarkedNullable }
            ?: return null
        val absent = required.filter { !body.has(it.wireName()) }
        if (absent.isEmpty()) return null

        val filled = body.deepCopy()
        for (parameter in absent) {
            filled.set(parameter.wireName(), placeholder(parameter) ?: return null)
        }
        val decoded = try {
            mapper.treeToValue(filled, type.java)
        } catch (_: JacksonException) {
            return null
        }

        val absentNames = absent.mapNotNull { it.name }.toSet()
        val present = validator.validate(decoded).filter { it.propertyPath.first().name !in absentNames }
        return AbsentFieldsException(absentNames.toList(), present)
    }

    // A type with no placeholder here falls back to Jackson's own failure,
    // INVALID_JSON_BODY: the answer before #18. Extend it when a model needs it.
    private fun placeholder(parameter: KParameter): JsonNode? = when (parameter.type.classifier) {
        String::class -> mapper.nodeFactory.stringNode("")
        Boolean::class -> mapper.nodeFactory.booleanNode(false)
        Int::class, Long::class -> mapper.nodeFactory.numberNode(0)
        else -> null
    }

    private fun KParameter.wireName(): String = findAnnotation<JsonProperty>()?.value ?: name.orEmpty()

    private class Replayed(private val headers: HttpHeaders, private val bytes: ByteArray) : HttpInputMessage {
        override fun getHeaders(): HttpHeaders = headers

        override fun getBody(): InputStream = ByteArrayInputStream(bytes)
    }

    private companion object {
        val GENERATED_MODELS: String = LocalLoginRequest::class.java.packageName
    }
}

// Kotlin property names, not wire names: ApiExceptionHandler spells both kinds
// of failure the same way. No stack trace: it is a verdict on the request, and
// ApiExceptionHandler answers it without logging.
class AbsentFieldsException(
    val absent: List<String>,
    val violations: List<ConstraintViolation<*>>,
) : RuntimeException("required fields absent: $absent", null, false, false)
