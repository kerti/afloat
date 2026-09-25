package dev.kerti.afloat.httperr

import dev.kerti.afloat.api.model.LocalLoginRequest
import org.springframework.core.MethodParameter
import org.springframework.core.Ordered
import org.springframework.core.annotation.Order
import org.springframework.http.HttpHeaders
import org.springframework.http.HttpInputMessage
import org.springframework.http.converter.HttpMessageConverter
import org.springframework.http.converter.HttpMessageNotReadableException
import org.springframework.web.bind.annotation.ControllerAdvice
import org.springframework.web.servlet.mvc.method.annotation.RequestBodyAdviceAdapter
import java.io.ByteArrayInputStream
import java.io.InputStream
import java.lang.reflect.Type
import java.nio.ByteBuffer
import java.nio.charset.CharacterCodingException
import java.nio.charset.StandardCharsets

// A request body is one JSON text in UTF-8 (RFC 8259 §8.1), read as Go's
// spec-validating middleware reads it (strictJSONBodyDecoder in
// backend/internal/httpserver/openapi_validate.go). Jackson alone is looser:
// it decodes overlong and out-of-range UTF-8, skips a byte order mark, and
// detects UTF-16 and UTF-32 from the bytes. Go answers INVALID_JSON_BODY for
// each of those bodies, so this does too. Utf8CharsetFilter has already
// relabelled whatever charset the header claimed as UTF-8.
//
// First of the request body advice, so AbsentFieldAdvice never reads a body
// this one refuses.
@ControllerAdvice
@Order(Ordered.HIGHEST_PRECEDENCE)
class JsonTextAdvice : RequestBodyAdviceAdapter() {

    override fun supports(
        methodParameter: MethodParameter,
        targetType: Type,
        converterType: Class<out HttpMessageConverter<*>>,
    ): Boolean = isGeneratedModel(targetType)

    // Buffering is bounded by MaxBodyFilter's 1 MiB; an over-long body throws
    // from readAllBytes and Spring answers it as unreadable, as before.
    override fun beforeBodyRead(
        inputMessage: HttpInputMessage,
        parameter: MethodParameter,
        targetType: Type,
        converterType: Class<out HttpMessageConverter<*>>,
    ): HttpInputMessage {
        val bytes = inputMessage.body.readAllBytes()
        if (!isJsonText(bytes)) {
            throw HttpMessageNotReadableException("request body is not UTF-8 JSON text", inputMessage)
        }
        return BufferedBody(inputMessage.headers, bytes)
    }
}

// Strict UTF-8, as utf8.Valid: the JDK's decoder refuses overlong forms,
// encoded surrogates and anything past U+10FFFF. No byte order mark, which
// encoding/json refuses as a stray character. No raw NUL: it can be neither
// whitespace nor unescaped inside a string, so it is never valid JSON, and it
// is what Jackson's UTF-16 and UTF-32 detection keys on.
internal fun isJsonText(bytes: ByteArray): Boolean {
    val text = try {
        StandardCharsets.UTF_8.newDecoder().decode(ByteBuffer.wrap(bytes))
    } catch (_: CharacterCodingException) {
        return false
    }
    return !text.startsWith('\uFEFF') && '\u0000' !in text
}

// Only the generated models are the contract's request bodies, as only a
// declared body is Go's middleware's to check.
internal fun isGeneratedModel(targetType: Type): Boolean =
    (targetType as? Class<*>)?.packageName == LocalLoginRequest::class.java.packageName

internal class BufferedBody(private val headers: HttpHeaders, private val bytes: ByteArray) : HttpInputMessage {
    override fun getHeaders(): HttpHeaders = headers

    override fun getBody(): InputStream = ByteArrayInputStream(bytes)
}
