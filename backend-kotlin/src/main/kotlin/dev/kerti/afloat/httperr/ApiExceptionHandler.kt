package dev.kerti.afloat.httperr

import dev.kerti.afloat.api.model.Error
import dev.kerti.afloat.api.model.ErrorCode
import jakarta.validation.ConstraintViolation
import org.slf4j.LoggerFactory
import org.springframework.http.HttpHeaders
import org.springframework.http.HttpStatus
import org.springframework.http.ResponseEntity
import org.springframework.http.converter.HttpMessageNotReadableException
import org.springframework.validation.FieldError
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

    // The envelope is flat and reports ONE field, so which one must not depend
    // on the run. Bean Validation collects violations in an unspecified order —
    // a HashSet, in practice — so `fieldErrors.first()` is whatever the JVM
    // felt like this time, and a body failing two fields would report either.
    //
    // The rule is: sort by the wire field name and take the first (#13 §3.5).
    // Alphabetical rather than declaration order because the generated model's
    // property order is the generator's to change, and it agrees with Go on the
    // one body that exists today — LocalLoginRequest is (email, password) in
    // both spellings.
    @ExceptionHandler(MethodArgumentNotValidException::class)
    fun handleValidation(e: MethodArgumentNotValidException): ResponseEntity<Error> {
        // Sorted by rule as well as field: two constraints can fail on the SAME
        // field (a @Size and a @Pattern), and then the field name alone still
        // leaves the reported rule to chance.
        val first = e.bindingResult.fieldErrors
            .map { snakeCase(it.field) to ruleOf(it) }
            .minWithOrNull(compareBy({ it.first }, { it.second }))
            ?: return badRequest(Error(ErrorCode.VALIDATION, null))
        return badRequest(
            Error(ErrorCode.VALIDATION, mapOf("field" to first.first, "rule" to first.second))
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
        private fun ruleOf(error: FieldError): String = when (val code = error.code) {
            "NotNull", "NotBlank", "NotEmpty" -> "required"
            "Email" -> "email"
            "Min", "DecimalMin" -> "min"
            "Max", "DecimalMax" -> "max"
            "Size" -> sizeBound(error)
            "Pattern" -> "pattern"
            else -> code?.lowercase() ?: "invalid"
        }

        // One @Size carries both bounds, where Go writes them as separate
        // `min=` and `max=` tags and reports whichever failed. Collapsing both
        // to "max" hands the frontend the key for "too long" when the value was
        // too short, so the bound is recovered from the constraint itself.
        private fun sizeBound(error: FieldError): String {
            val min = error.constraintAttribute("min") ?: return "max"
            val length = when (val rejected = error.rejectedValue) {
                is CharSequence -> rejected.length
                is Collection<*> -> rejected.size
                is Map<*, *> -> rejected.size
                is Array<*> -> rejected.size
                else -> return "max"
            }
            return if (length < min) "min" else "max"
        }

        private fun FieldError.constraintAttribute(name: String): Int? =
            if (contains(ConstraintViolation::class.java)) {
                unwrap(ConstraintViolation::class.java).constraintDescriptor.attributes[name] as? Int
            } else {
                null
            }
    }
}
