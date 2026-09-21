package dev.kerti.afloat.auth

import org.slf4j.LoggerFactory
import org.springframework.security.crypto.argon2.Argon2PasswordEncoder
import org.springframework.stereotype.Component

@Component
class PasswordService {

    fun hash(password: String): String = checkNotNull(encoder.encode(password))

    // A corrupt row must fail the login, not crash the handler. The decoder
    // throws whatever the malformation happens to produce - IllegalArgument for
    // a bad number, ArrayIndexOutOfBounds for missing segments,
    // UnsupportedOperation for an algorithm or version it does not implement -
    // so the catch is by outcome, not by exception type. Go's VerifyPassword
    // returns a bool for the same reason.
    fun verify(password: String, phc: String): Boolean = try {
        encoder.matches(password, phc)
    } catch (e: RuntimeException) {
        log.warn("password verify: unusable stored hash", e)
        false
    }

    companion object {
        private val log = LoggerFactory.getLogger(PasswordService::class.java)

        // Argon2id parameters fixed by BOOTSTRAP.md §5.1 and identical to Balances:
        // m=19456 KiB, t=2, p=1, salt 16, key 32. A hash written here must verify
        // in the Go backend, which reads these out of the PHC string.
        private val encoder = Argon2PasswordEncoder(16, 32, 1, 19 * 1024, 2)

        // Verified against when no credentials exist, so an unknown or dormant
        // address costs the same Argon2id work as a real one. Generated once
        // from a value nobody knows.
        val dummyHash: String by lazy {
            checkNotNull(
                encoder.encode("this password matches nothing, by construction")
            )
        }
    }
}
