package dev.kerti.afloat.httperr

import com.fasterxml.jackson.annotation.JsonProperty
import io.kotest.assertions.throwables.shouldThrow
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe
import jakarta.validation.Validation
import jakarta.validation.constraints.Size
import org.springframework.core.MethodParameter
import org.springframework.http.HttpHeaders
import org.springframework.http.HttpInputMessage
import org.springframework.http.converter.StringHttpMessageConverter
import tools.jackson.databind.json.JsonMapper
import tools.jackson.module.kotlin.KotlinModule

// A required field whose type is nullable (`required` plus `nullable: true` in
// the contract). kin-openapi reports it `required` when absent, so Kotlin must
// too, rather than leaving it to Jackson's decode.
data class NullableRequired(
    @param:JsonProperty("note", required = true) val note: String?,
    @get:Size(min = 1)
    @param:JsonProperty("code", required = true) val code: String,
)

class AbsentFieldAdviceSpec : StringSpec({

    val mapper = JsonMapper.builder().addModule(KotlinModule.Builder().build()).build()
    val advice = AbsentFieldAdvice(mapper, Validation.buildDefaultValidatorFactory().validator)

    class Target {
        @Suppress("UNUSED_PARAMETER")
        fun handle(body: NullableRequired) = Unit
    }

    val parameter = MethodParameter(Target::class.java.getDeclaredMethod("handle", NullableRequired::class.java), 0)

    fun read(body: String) = advice.beforeBodyRead(
        object : HttpInputMessage {
            override fun getHeaders() = HttpHeaders()

            override fun getBody() = body.byteInputStream()
        },
        parameter,
        NullableRequired::class.java,
        StringHttpMessageConverter::class.java,
    )

    "reports an absent nullable required field beside the present fields' violations" {
        val e = shouldThrow<AbsentFieldsException> { read("""{"code":""}""") }

        e.absent shouldBe listOf("note")
        e.violations.map { it.propertyPath.toString() } shouldBe listOf("code")
    }

    "passes a body with every required field through untouched" {
        read("""{"note":null,"code":"x"}""").body.readAllBytes().decodeToString() shouldBe """{"note":null,"code":"x"}"""
    }
})
