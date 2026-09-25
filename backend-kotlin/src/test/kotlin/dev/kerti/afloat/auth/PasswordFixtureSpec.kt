package dev.kerti.afloat.auth

import dev.kerti.afloat.testsupport.loggedBy
import io.kotest.assertions.withClue
import io.kotest.core.spec.style.StringSpec
import io.kotest.datatest.withData
import io.kotest.matchers.collections.shouldBeEmpty
import io.kotest.matchers.collections.shouldNotBeEmpty
import io.kotest.matchers.shouldBe
import org.bouncycastle.crypto.generators.Argon2BytesGenerator
import org.bouncycastle.crypto.params.Argon2Parameters
import org.springframework.security.crypto.argon2.Argon2PasswordEncoder
import tools.jackson.databind.JsonNode
import tools.jackson.databind.json.JsonMapper
import java.io.File
import java.time.Duration
import java.util.Base64

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
        // verifyHoldingPermit catches whatever the decoder throws, so a throw
        // shows only in the log: silence is what "never an exception" means here.
        withClue(case.text("phc")) {
            val (outer, fromService) = loggedBy(PasswordService::class.java) {
                loggedBy(Argon2PasswordEncoder::class.java) {
                    service.verifyHoldingPermit(case.text("password"), case.text("phc"))
                }
            }
            val (verified, fromDecoder) = outer
            verified shouldBe false
            fromService.shouldBeEmpty()
            fromDecoder.shouldBeEmpty()
        }
    }

    // A reject row that tests a shape rule is worth something only while its
    // tag is the one a lenient parser would compute: with any other tag it is
    // refused by the mismatch, and the rule it names goes untested. So the
    // BouncyCastle-minted tags are recomputed here from whatever the string
    // says. Go's argon2_fixture_test.go does the same for the Go-minted ones.
    val lenientPhc = Regex("""^[$](argon2id|argon2i|argon2d)[$]v=(\d+)[$]m=(\d+),t=(\d+),p=(\d+)[$]([^$]+)[$]([^$]+)$""")
    val bouncyCastleMinted = fixture.get("rejects").toList().filter { it.get("minted_by")?.asString() == "bouncycastle" }

    "has BouncyCastle-minted reject rows" {
        bouncyCastleMinted.shouldNotBeEmpty()
    }

    withData(
        nameFn = { "carries a genuine BouncyCastle tag: ${it.text("why")}" },
        bouncyCastleMinted,
    ) { case ->
        withClue(case.text("phc")) {
            val (type, version, m, t, p, salt, tag) = checkNotNull(lenientPhc.matchEntire(case.text("phc"))).destructured
            val expected = Base64.getDecoder().decode(tag)
            val params = Argon2Parameters.Builder(
                when (type) {
                    "argon2id" -> Argon2Parameters.ARGON2_id
                    "argon2i" -> Argon2Parameters.ARGON2_i
                    else -> Argon2Parameters.ARGON2_d
                }
            )
                .withVersion(version.toInt())
                .withMemoryAsKB(m.toInt())
                .withIterations(t.toInt())
                .withParallelism(p.toInt())
                .withSalt(Base64.getDecoder().decode(salt))
                .build()
            val computed = ByteArray(expected.size)
            Argon2BytesGenerator().apply { init(params) }.generateBytes(case.text("password").toByteArray(), computed)
            computed shouldBe expected
        }
    }
})
