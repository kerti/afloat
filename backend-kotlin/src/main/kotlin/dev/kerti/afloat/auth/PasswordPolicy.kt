package dev.kerti.afloat.auth

import org.springframework.stereotype.Component

// Whether a password may be SET. Nothing calls this yet — there is no
// registration or password-set endpoint in either backend — but Go carries the
// same rules in internal/auth/password.go, and a policy that exists on one side
// only is a divergence waiting for whichever backend ships registration first.
//
// A floor and a denylist, no composition rules: forced symbols push people
// towards predictable substitutions, and length is what actually helps.
@Component
class PasswordPolicy {

    enum class Failure { TOO_SHORT, TOO_LONG, COMMON }

    // Null when the password may be set. Go returns a sentinel error for each
    // of the same three cases and the caller maps it to the envelope; the
    // mapping is the caller's job on both sides.
    fun validate(password: String): Failure? = when {
        // Code points, not UTF-16 units and not bytes: an Indonesian or any
        // non-ASCII passphrase must not be penalised for encoding wider, and
        // String.length would count an emoji or a rarer script twice. Go counts
        // runes here for the same reason.
        password.codePointCount(0, password.length) < MIN_CODE_POINTS -> Failure.TOO_SHORT
        password.codePointCount(0, password.length) > MAX_CODE_POINTS -> Failure.TOO_LONG
        CommonPasswords.contains(password) -> Failure.COMMON
        else -> null
    }

    companion object {
        const val MIN_CODE_POINTS = 10

        // Argon2id's cost does not grow with password length, so this is not
        // about hashing time — it bounds what gets read and held in memory.
        const val MAX_CODE_POINTS = 1024
    }
}
