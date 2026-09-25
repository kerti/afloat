package dev.kerti.afloat.httperr

import jakarta.validation.ConstraintValidator
import jakarta.validation.ConstraintValidatorContext
import jakarta.validation.constraints.Email
import jakarta.validation.constraints.Size
import org.hibernate.validator.HibernateValidatorConfiguration
import org.springframework.boot.jackson.autoconfigure.JsonMapperBuilderCustomizer
import org.springframework.boot.validation.autoconfigure.ValidationConfigurationCustomizer
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import tools.jackson.databind.cfg.CoercionAction
import tools.jackson.databind.cfg.CoercionInputShape
import tools.jackson.databind.type.LogicalType

// Where the generated models' own reading of the contract differs from Go's
// spec-validating middleware (backend/internal/httpserver/openapi_validate.go),
// and #18 ruled for Go's.
@Configuration
class RequestContractConfiguration {

    // `format: email` trims, then validates (the ruling on #18): a padded
    // address logs in. The trim lives inside the check rather than in front of
    // it, exactly as Go's does, so @Size still counts the padding. The address
    // rule itself is Go's too (TrimmedEmailValidator).
    @Bean
    fun trimmedEmailValidation(): ValidationConfigurationCustomizer = ValidationConfigurationCustomizer { configuration ->
        val hibernate = configuration as HibernateValidatorConfiguration
        val mapping = hibernate.createConstraintMapping()
        mapping.constraintDefinition(Email::class.java)
            .includeExistingValidators(false)
            .validatedBy(TrimmedEmailValidator::class.java)
        hibernate.addMapping(mapping)
    }

    // `minLength` and `maxLength` count characters as JSON Schema does, one per
    // code point, which is what kin-openapi's loop counts (its comment says
    // UTF-16 units; the loop does not). Hibernate's @Size counts UTF-16 units,
    // so a password of emoji was twice as long here as in Go. The existing
    // validators stay for collections, maps and arrays; for a String, the more
    // specific type, this one is chosen.
    @Bean
    fun codePointSizeValidation(): ValidationConfigurationCustomizer = ValidationConfigurationCustomizer { configuration ->
        val hibernate = configuration as HibernateValidatorConfiguration
        val mapping = hibernate.createConstraintMapping()
        mapping.constraintDefinition(Size::class.java)
            .includeExistingValidators(true)
            .validatedBy(CodePointSizeValidator::class.java)
        hibernate.addMapping(mapping)
    }

    // `type: string` means a string. Jackson otherwise turns 5 or true into
    // "5" or "true" for a String property, where Go answers INVALID_JSON_BODY
    // for the wrong type.
    @Bean
    fun noScalarToStringCoercion(): JsonMapperBuilderCustomizer = JsonMapperBuilderCustomizer { builder ->
        builder.withCoercionConfig(LogicalType.Textual) { config ->
            config.setCoercion(CoercionInputShape.Integer, CoercionAction.Fail)
                .setCoercion(CoercionInputShape.Float, CoercionAction.Fail)
                .setCoercion(CoercionInputShape.Boolean, CoercionAction.Fail)
        }
    }
}

// The contract's `format: email`, as Go's spec-validating middleware reads it:
// trimmed as Go's strings.TrimSpace trims, then matched against the WHATWG
// <input type=email> rule. contract/testdata/email.json pins the two backends
// to the same answers (TrimmedEmailValidatorSpec). Hibernate's own @Email
// accepts addresses this rule does not (a@b_c.com, a@[127.0.0.1],
// üser@example.com) and calls "" valid, so it is replaced, not wrapped.
class TrimmedEmailValidator : ConstraintValidator<Email, CharSequence> {
    override fun isValid(value: CharSequence?, context: ConstraintValidatorContext?): Boolean =
        value == null || EMAIL_ADDRESS.matches(value.trimSpace())

    private companion object {
        // Go's copy is emailAddress in openapi_validate.go.
        val EMAIL_ADDRESS = Regex(
            "[a-zA-Z0-9.!#\$%&'*+/=?^_`{|}~-]+" +
                """@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*""",
        )
    }
}

// @Size on a String, measured in code points (codePointSizeValidation).
class CodePointSizeValidator : ConstraintValidator<Size, String> {
    private var bounds = 0..Int.MAX_VALUE

    override fun initialize(constraint: Size) {
        bounds = constraint.min..constraint.max
    }

    override fun isValid(value: String?, context: ConstraintValidatorContext?): Boolean =
        value == null || value.codePointCount(0, value.length) in bounds
}

// Go's strings.TrimSpace, not Kotlin's trim(): the two disagree on U+0085,
// which Go trims, and U+001C..U+001F, which Kotlin does. An address both
// backends must read the same way is trimmed with this.
fun CharSequence.trimSpace(): String = trim(::isGoSpace).toString()

// unicode.IsSpace: Latin-1's six ASCII spaces plus NEL and NBSP, and the
// Unicode White_Space characters above Latin-1.
private fun isGoSpace(c: Char): Boolean = when (c) {
    '\t', '\n', '\u000B', '\u000C', '\r', ' ', '\u0085', '\u00A0' -> true
    '\u1680', in '\u2000'..'\u200A', '\u2028', '\u2029', '\u202F', '\u205F', '\u3000' -> true
    else -> false
}
