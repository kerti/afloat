package dev.kerti.afloat.httperr

import jakarta.validation.ConstraintValidator
import jakarta.validation.ConstraintValidatorContext
import jakarta.validation.constraints.Email
import org.hibernate.validator.HibernateValidatorConfiguration
import org.hibernate.validator.internal.constraintvalidators.bv.EmailValidator
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
    // it, exactly as Go's does, so @Size still counts the padding.
    @Bean
    fun trimmedEmailValidation(): ValidationConfigurationCustomizer = ValidationConfigurationCustomizer { configuration ->
        val hibernate = configuration as HibernateValidatorConfiguration
        val mapping = hibernate.createConstraintMapping()
        mapping.constraintDefinition(Email::class.java)
            .includeExistingValidators(false)
            .validatedBy(TrimmedEmailValidator::class.java)
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

// Hibernate's own address rules, applied to the trimmed value. Two departures,
// both Go's: surrounding whitespace is not part of the address, and an empty
// one is not an address at all — Hibernate calls "" valid and leaves it to
// @NotBlank, which the generator never emits.
class TrimmedEmailValidator : ConstraintValidator<Email, CharSequence> {
    private val delegate = EmailValidator()

    override fun initialize(annotation: Email) = delegate.initialize(annotation)

    override fun isValid(value: CharSequence?, context: ConstraintValidatorContext): Boolean {
        if (value == null) return true
        val trimmed = value.trim()
        return trimmed.isNotEmpty() && delegate.isValid(trimmed, context)
    }
}
