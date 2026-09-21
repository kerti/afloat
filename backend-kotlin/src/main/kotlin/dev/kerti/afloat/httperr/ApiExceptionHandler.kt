package dev.kerti.afloat.httperr

import dev.kerti.afloat.api.model.Error
import dev.kerti.afloat.api.model.ErrorCode
import org.slf4j.LoggerFactory
import org.springframework.http.HttpHeaders
import org.springframework.http.HttpStatus
import org.springframework.http.ResponseEntity
import org.springframework.http.converter.HttpMessageNotReadableException
import org.springframework.web.HttpMediaTypeNotSupportedException
import org.springframework.web.HttpRequestMethodNotSupportedException
import org.springframework.web.bind.MethodArgumentNotValidException
import org.springframework.web.bind.annotation.ExceptionHandler
import org.springframework.web.bind.annotation.RestControllerAdvice
import org.springframework.web.servlet.resource.NoResourceFoundException

// Every error response is a single envelope (codes, not messages); the cause is
// logged where it happened. Only the first failing field is reported, matching
// the contract and Go's WriteValidation.
@RestControllerAdvice
class ApiExceptionHandler {

    @ExceptionHandler(ApiException::class)
    fun handleApiException(e: ApiException): ResponseEntity<Error> {
        // A 5xx raised deliberately still has a cause worth keeping; the
        // handler cannot know the detail, so callers log that themselves.
        if (e.status >= 500) log.error("api exception: {}", e.code.value, e)
        val headers = HttpHeaders()
        e.retryAfterSeconds?.let { headers.set(HttpHeaders.RETRY_AFTER, it.toString()) }
        return ResponseEntity(Error(e.code, e.args), headers, HttpStatus.valueOf(e.status))
    }

    @ExceptionHandler(MethodArgumentNotValidException::class)
    fun handleValidation(e: MethodArgumentNotValidException): ResponseEntity<Error> {
        val first = e.bindingResult.fieldErrors.firstOrNull()
            ?: return badRequest(Error(ErrorCode.VALIDATION, null))
        return badRequest(
            Error(ErrorCode.VALIDATION, mapOf("field" to snakeCase(first.field), "rule" to ruleOf(first.code)))
        )
    }

    @ExceptionHandler(HttpMessageNotReadableException::class)
    fun handleUnreadable(e: HttpMessageNotReadableException): ResponseEntity<Error> =
        badRequest(Error(ErrorCode.INVALID_JSON_BODY, null))

    // A body that was never JSON: decode could not even start.
    @ExceptionHandler(HttpMediaTypeNotSupportedException::class)
    fun handleMediaType(e: HttpMediaTypeNotSupportedException): ResponseEntity<Error> =
        badRequest(Error(ErrorCode.INVALID_JSON_BODY, null))

    // No NOT_FOUND code exists in the contract enum (deliberately), so an
    // unknown route gets a bare 404, exactly the status Go's chi returns.
    @ExceptionHandler(NoResourceFoundException::class)
    fun handleNoResource(e: NoResourceFoundException): ResponseEntity<Error> =
        ResponseEntity.status(HttpStatus.NOT_FOUND).build()

    // Likewise 405: the catch-all below would otherwise turn Spring's own
    // method-not-allowed into a 500.
    @ExceptionHandler(HttpRequestMethodNotSupportedException::class)
    fun handleMethodNotAllowed(e: HttpRequestMethodNotSupportedException): ResponseEntity<Error> =
        ResponseEntity.status(HttpStatus.METHOD_NOT_ALLOWED).build()

    @ExceptionHandler(Exception::class)
    fun handleUnexpected(e: Exception): ResponseEntity<Error> {
        log.error("unhandled exception", e)
        return ResponseEntity(Error(ErrorCode.INTERNAL, null), HttpStatus.INTERNAL_SERVER_ERROR)
    }

    private fun badRequest(body: Error): ResponseEntity<Error> =
        ResponseEntity(body, HttpStatus.BAD_REQUEST)

    companion object {
        private val log = LoggerFactory.getLogger(ApiExceptionHandler::class.java)

        // The wire name, not the Kotlin one: the frontend's catalogue and the
        // contract both speak snake_case (Go's jsonFieldName).
        private val CAMEL_BOUNDARY = Regex("([a-z0-9])([A-Z])")

        private fun snakeCase(field: String): String =
            field.replace(CAMEL_BOUNDARY, "$1_$2").lowercase()

        // Go reports the validator tag, Bean Validation the constraint class.
        // One vocabulary, or the i18n catalogue needs two sets of keys.
        private fun ruleOf(code: String?): String = when (code) {
            "NotNull", "NotBlank", "NotEmpty" -> "required"
            "Email" -> "email"
            "Min", "DecimalMin" -> "min"
            "Max", "DecimalMax", "Size" -> "max"
            "Pattern" -> "pattern"
            else -> code?.lowercase() ?: "invalid"
        }
    }
}
