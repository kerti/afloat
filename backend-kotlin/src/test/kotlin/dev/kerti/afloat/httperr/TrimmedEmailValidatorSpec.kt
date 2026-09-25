package dev.kerti.afloat.httperr

import io.kotest.assertions.withClue
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.collections.shouldNotBeEmpty
import io.kotest.matchers.shouldBe
import tools.jackson.databind.json.JsonMapper
import java.io.File

// `format: email` answers every address in the shared fixture exactly as Go's
// spec-validating middleware does (login_validation_test.go,
// TestEmailFormatMatchesTheSharedFixture).
class TrimmedEmailValidatorSpec : StringSpec({

    val fixture = JsonMapper().readTree(File(System.getProperty("afloat.contract.testdata"), "email.json"))
    val validator = TrimmedEmailValidator()

    fun addresses(list: String): List<String> = fixture.get(list).let { node -> (0 until node.size()).map { node.get(it).asString() } }

    "accepts every address the fixture calls valid" {
        addresses("valid").shouldNotBeEmpty().forEach { address ->
            withClue(address) { validator.isValid(address, null) shouldBe true }
        }
    }

    "rejects every address the fixture calls invalid" {
        addresses("invalid").shouldNotBeEmpty().forEach { address ->
            withClue(address) { validator.isValid(address, null) shouldBe false }
        }
    }

    // Absent is @NotNull's to answer, and the generator never emits one:
    // AbsentFieldAdvice reports it as `required`.
    "leaves a null to other constraints" {
        validator.isValid(null, null) shouldBe true
    }
})
