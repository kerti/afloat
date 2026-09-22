package dev.kerti.afloat.auth

import io.kotest.assertions.withClue
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe
import io.kotest.matchers.shouldNotBe
import java.nio.file.Path

// #14 tests 7-10. Nothing calls the policy yet in either backend — there is no
// registration or password-set endpoint — so these pin the rules themselves,
// against Go's internal/auth/password.go.
class PasswordPolicySpec : StringSpec({

    val policy = PasswordPolicy()

    // passwordPolicyRejectsShorterThanTenCharacters
    "rejects a password shorter than ten code points" {
        // Not a repeated character on either side of the boundary: "aaaaaaaaaa"
        // is itself a denylist entry, so it would answer COMMON and say nothing
        // about the floor.
        policy.validate("kucingore") shouldBe PasswordPolicy.Failure.TOO_SHORT
        policy.validate("kucingoren") shouldBe null
    }

    // The floor is counted in CODE POINTS, so an Indonesian or any non-ASCII
    // passphrase is not penalised for encoding wider. In Kotlin the trap is
    // narrower than Go's: String.length counts UTF-16 units, so ten astral
    // characters measure twenty and a nine-character passphrase would pass.
    "counts the floor in code points, not UTF-16 units" {
        // Ten code points, twenty UTF-16 units: accepted on the count, and the
        // clue is here because a naive String.length makes this pass anyway.
        val tenAstral = "😀".repeat(10)
        withClue("length=${tenAstral.length}, code points=10") {
            policy.validate(tenAstral) shouldBe null
        }

        // Nine code points, eighteen UTF-16 units: must be rejected. This is
        // the case String.length gets wrong.
        val nineAstral = "😀".repeat(9)
        withClue("length=${nineAstral.length}, code points=9") {
            policy.validate(nineAstral) shouldBe PasswordPolicy.Failure.TOO_SHORT
        }
    }

    // passwordPolicyRejectsLongerThan1024CodePoints
    "rejects a password longer than 1024 code points" {
        policy.validate("a".repeat(1024)) shouldBe null
        policy.validate("a".repeat(1025)) shouldBe PasswordPolicy.Failure.TOO_LONG
    }

    "counts the cap in code points, not UTF-16 units" {
        // 1024 code points but 2048 UTF-16 units: under the cap, and a
        // String.length implementation would reject it.
        policy.validate("😀".repeat(1024)) shouldBe null
        policy.validate("😀".repeat(1025)) shouldBe PasswordPolicy.Failure.TOO_LONG
    }

    // passwordPolicyRejectsACommonPassword
    "rejects a password on the shared denylist" {
        policy.validate("password1234") shouldBe PasswordPolicy.Failure.COMMON
    }

    "matches the denylist case-insensitively" {
        // Password1234 and password1234 are the same guess, and an attacker
        // tries both.
        policy.validate("PASSWORD1234") shouldBe PasswordPolicy.Failure.COMMON
        policy.validate("PaSsWoRd1234") shouldBe PasswordPolicy.Failure.COMMON
    }

    // #14 test 9: read the same file the Go backend reads, do not retype it.
    // shared/common_passwords.txt is canonical and scripts/sync-denylist.sh
    // copies it into both backends; `make check` gates the copies. This asserts
    // the copy on THIS classpath actually carries the canonical entries, so a
    // build that shipped a stale or empty resource fails here rather than
    // silently accepting every breached password.
    "loads every entry of the canonical denylist" {
        val canonical = Path.of("../shared/common_passwords.txt").toFile()
            .readLines()
            .map { it.trim().lowercase() }
            .filter { it.isNotEmpty() && !it.startsWith("#") }

        withClue("shared/common_passwords.txt parsed to ${canonical.size} entries") {
            canonical.size shouldNotBe 0
        }
        canonical.forEach { entry ->
            withClue(entry) { CommonPasswords.contains(entry) shouldBe true }
        }
    }

    // passwordPolicyHasNoCompositionRules
    "accepts a long lowercase passphrase with no digits or symbols" {
        // Forced symbols push people towards predictable substitutions; length
        // is what actually helps.
        policy.validate("correct horse battery staple") shouldBe null
        policy.validate("kucing oren lompat pagar") shouldBe null
    }

    "reports the first failing rule only" {
        // A short password that is also on the denylist is TOO_SHORT: Go checks
        // length before the denylist, and the envelope carries one rule.
        policy.validate("pass") shouldBe PasswordPolicy.Failure.TOO_SHORT
    }
})
