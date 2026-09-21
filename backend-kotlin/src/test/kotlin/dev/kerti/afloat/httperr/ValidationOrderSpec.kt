package dev.kerti.afloat.httperr

import dev.kerti.afloat.api.model.ErrorCode
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe
import org.springframework.core.MethodParameter
import org.springframework.validation.BeanPropertyBindingResult
import org.springframework.validation.FieldError
import org.springframework.web.bind.MethodArgumentNotValidException

// The repetition test in LoginSpec can only ever say "this JVM happened to be
// stable". This one hands the handler its violations in the WRONG order and
// asserts it still reports the same field, which is the actual guarantee #13
// §3.5 asks for: the choice is sorted, not incidental.
class ValidationOrderSpec : StringSpec({

    val handler = ApiExceptionHandler()

    // A stand-in for a controller method taking a request body; only its
    // signature is needed, to build a MethodParameter.
    class Target {
        @Suppress("UNUSED_PARAMETER")
        fun handle(body: String) = Unit
    }

    val parameter = MethodParameter(Target::class.java.getDeclaredMethod("handle", String::class.java), 0)

    fun exceptionWith(vararg fields: Pair<String, String>): MethodArgumentNotValidException {
        val binding = BeanPropertyBindingResult("body", "body")
        fields.forEach { (field, code) ->
            binding.addError(
                FieldError("body", field, null, false, arrayOf(code), null, null),
            )
        }
        return MethodArgumentNotValidException(parameter, binding)
    }

    "reports the first field by name, whatever order the violations arrive in" {
        val forwards = handler.handleValidation(exceptionWith("email" to "Email", "password" to "NotBlank"))
        val backwards = handler.handleValidation(exceptionWith("password" to "NotBlank", "email" to "Email"))

        forwards.body shouldBe backwards.body
        forwards.body?.code shouldBe ErrorCode.VALIDATION
        forwards.body?.args shouldBe mapOf("field" to "email", "rule" to "email")
    }

    // Two constraints can fail on ONE field, and then the field name alone
    // still leaves the reported rule to chance.
    "reports the first rule by name when one field fails twice" {
        val forwards = handler.handleValidation(exceptionWith("password" to "Pattern", "password" to "NotBlank"))
        val backwards = handler.handleValidation(exceptionWith("password" to "NotBlank", "password" to "Pattern"))

        forwards.body shouldBe backwards.body
        // "pattern" sorts after "required"; the point is that it is always the
        // same one, not which one it is.
        forwards.body?.args shouldBe mapOf("field" to "password", "rule" to "pattern")
    }

    // The wire name, not the Kotlin one. Sorting happens on the snake_case
    // spelling, so the order cannot depend on how the generator capitalises.
    "sorts on the wire spelling of the field" {
        val result = handler.handleValidation(
            exceptionWith("displayName" to "NotBlank", "allowanceMode" to "NotBlank"),
        )

        result.body?.args shouldBe mapOf("field" to "allowance_mode", "rule" to "required")
    }
})
