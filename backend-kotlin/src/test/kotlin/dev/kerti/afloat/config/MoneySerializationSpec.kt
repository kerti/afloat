package dev.kerti.afloat.config

import dev.kerti.afloat.testsupport.DatabaseSpec
import io.kotest.assertions.withClue
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldNotContain
import org.springframework.beans.factory.annotation.Autowired
import tools.jackson.databind.ObjectMapper
import java.math.BigDecimal

// #13 tests 25-27, against the application's own ObjectMapper rather than one
// built for the occasion: the guarantee is about the configuration the web
// layer actually serialises through, and a mapper assembled in the test would
// assert only that the test knows how to assemble one.
//
// No money-carrying endpoint has a BigDecimal on the wire — every money field
// in the contract is `type: string`, so the generated models carry String. That
// is exactly why this is asserted now: the configuration is being set now, and
// an unasserted configuration is one refactor from silently reverting.
class MoneySerializationSpec : DatabaseSpec() {

    @Autowired
    private lateinit var objectMapper: ObjectMapper

    data class MoneyHolder(val amount: BigDecimal?)

    private fun json(value: BigDecimal?): String = objectMapper.writeValueAsString(MoneyHolder(value))

    init {
        // bigDecimalSerialisesAsAString
        "serialises a BigDecimal as a string, never a JSON number" {
            // Quoted. A JSON number here is a bug, not a formatting quirk
            // (non-negotiable 1): the frontend would parse it as a double and
            // lose the last rupiah of a large budget.
            json(BigDecimal("1234.5600")) shouldBe """{"amount":"1234.5600"}"""
        }

        // bigDecimalNeverUsesScientificNotation
        "never uses scientific notation" {
            val body = json(BigDecimal("25000000.0000"))

            body shouldBe """{"amount":"25000000.0000"}"""
            withClue(body) { body shouldNotContain "E" }
        }

        "never uses scientific notation for a value that toString would exponentiate" {
            // BigDecimal.toString switches to exponent form once the scale and
            // magnitude line up; toPlainString does not. This is the pair that
            // tells the two apart.
            val exponential = BigDecimal("1E+9")
            withClue("toString gives ${exponential}") {
                json(exponential) shouldBe """{"amount":"1000000000"}"""
            }
        }

        // bigDecimalPreservesTrailingZeroesToFourPlaces
        "preserves the scale the column carries" {
            // DECIMAL(20,4) means 1.5 comes back from Postgres as 1.5000, and
            // it has to stay that way on the wire: a client formatting currency
            // reads the scale, and "1.5" and "1.5000" are different statements
            // about precision.
            json(BigDecimal("1.5000")) shouldBe """{"amount":"1.5000"}"""
            json(BigDecimal("0.0001")) shouldBe """{"amount":"0.0001"}"""
        }

        // The null case stays null-shaped, not "0" or "": the household may
        // never have supplied one, which is a different statement from zero.
        "leaves an absent amount absent" {
            // default-property-inclusion=non_null omits it, matching Go's
            // omitempty on *string (internal/auth/response.go).
            json(null) shouldBe "{}"
        }
    }
}
