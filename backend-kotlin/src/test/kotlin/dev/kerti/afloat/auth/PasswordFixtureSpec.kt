package dev.kerti.afloat.auth

import io.kotest.assertions.throwables.shouldNotThrowAny
import io.kotest.assertions.withClue
import io.kotest.core.spec.style.StringSpec
import io.kotest.datatest.withData
import io.kotest.matchers.collections.shouldNotBeEmpty
import io.kotest.matchers.shouldBe
import tools.jackson.databind.JsonNode
import tools.jackson.databind.json.JsonMapper
import java.io.File
import java.time.Duration

// The PHC strings both backends must read alike (contract/testdata/argon2.json,
// issue #16). Go's argon2_fixture_test.go reads the same file: a hash either
// backend wrote must verify in the other, and a string neither should accept
// must be refused by both - as "does not verify", never as an exception.
class PasswordFixtureSpec : StringSpec({

    val fixture = JsonMapper().readTree(File(System.getProperty("afloat.contract.testdata"), "argon2.json"))
    val service = PasswordService(Duration.ofSeconds(30))

    fun JsonNode.text(field: String): String = get(field).asString()

    "reads a non-empty fixture" {
        // A fixture that asserts nothing looks like coverage.
        fixture.get("verifies").toList().shouldNotBeEmpty()
        fixture.get("rejects").toList().shouldNotBeEmpty()
    }

    withData(
        nameFn = { "verifies a ${it.text("minted_by")}-minted hash at cost ${it.text("cost")}: ${it.text("password")}" },
        fixture.get("verifies").toList(),
    ) { case ->
        withClue(case.text("phc")) {
            service.verifyHoldingPermit(case.text("password"), case.text("phc")) shouldBe true
        }
    }

    withData(
        nameFn = { "refuses: ${it.text("why")}" },
        fixture.get("rejects").toList(),
    ) { case ->
        withClue(case.text("phc")) {
            shouldNotThrowAny { service.verifyHoldingPermit(case.text("password"), case.text("phc")) } shouldBe false
        }
    }
})
