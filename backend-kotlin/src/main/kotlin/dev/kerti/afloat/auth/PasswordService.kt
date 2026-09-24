package dev.kerti.afloat.auth

import dev.kerti.afloat.config.AppConfig
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.security.crypto.argon2.Argon2PasswordEncoder
import org.springframework.stereotype.Component
import java.time.Duration
import java.util.concurrent.Semaphore
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

// Thrown when no Argon2 permit came free within the wait. The password was
// never checked, so this is neither a match nor a mismatch.
class HashingUnavailableException : RuntimeException("no Argon2 permit within the wait")

@Component
class PasswordService internal constructor(private val permitWait: Duration) {

    // A servlet request carries no deadline to wait against, so the wait is
    // HTTP_WRITE_TIMEOUT, counted from when it starts. Go's wait gets whatever
    // is left of the request's handler timeout, so a Kotlin login can run
    // longer end to end than a Go one (BOOTSTRAP.md §5.1).
    @Autowired
    constructor(appConfig: AppConfig) : this(appConfig.writeTimeout)

    // Each hash holds m=19456 KiB for its duration, allocated per call, so
    // without a bound an unauthenticated caller chooses the heap's peak (#33).
    // Fair, so a queued login is served in arrival order.
    private val permits = Semaphore(ARGON_CONCURRENCY_CAP, true)

    // Exact, updated as each call takes a permit, so a test asserts the bound
    // itself rather than inferring it from memory or wall-clock time.
    private val inFlight = AtomicInteger()
    private val peak = AtomicInteger()

    internal val peakInFlight: Int get() = peak.get()

    internal fun resetPeakInFlight() = peak.set(0)

    fun hash(password: String): String = withPermit { checkNotNull(encoder.encode(password)) }

    // A corrupt row must fail the login, not crash the handler. The decoder
    // throws whatever the malformation happens to produce - IllegalArgument for
    // a bad number, ArrayIndexOutOfBounds for missing segments,
    // UnsupportedOperation for an algorithm or version it does not implement -
    // so the catch is by outcome, not by exception type. Go's VerifyPassword
    // returns a bool for the same reason.
    fun verify(password: String, phc: String): Boolean = withPermit {
        try {
            encoder.matches(password, phc)
        } catch (e: RuntimeException) {
            log.warn("password verify: unusable stored hash", e)
            false
        }
    }

    internal fun <T> withPermit(block: () -> T): T {
        if (!permits.tryAcquire(permitWait.toNanos(), TimeUnit.NANOSECONDS)) {
            throw HashingUnavailableException()
        }
        try {
            peak.accumulateAndGet(inFlight.incrementAndGet(), ::maxOf)
            return block()
        } finally {
            inFlight.decrementAndGet()
            permits.release()
        }
    }

    companion object {
        private val log = LoggerFactory.getLogger(PasswordService::class.java)

        // Fixed by the #33 ruling, not an operator knob: 4 x 19 MiB ~ 76 MiB of
        // Argon2 memory at most (BOOTSTRAP.md §5.1). Go's argonConcurrencyCap.
        internal const val ARGON_CONCURRENCY_CAP = 4

        // Argon2id parameters fixed by BOOTSTRAP.md §5.1 and identical to Balances:
        // m=19456 KiB, t=2, p=1, salt 16, key 32. A hash written here must verify
        // in the Go backend, which reads these out of the PHC string.
        private val encoder = Argon2PasswordEncoder(16, 32, 1, 19 * 1024, 2)

        // Verified against when no credentials exist, so an unknown or dormant
        // address costs the same Argon2id work as a real one. Generated once
        // from a value nobody knows, when the class loads at startup, as Go's
        // is at package init: generated lazily, the first unknown address
        // paid for two hashes and answered slower than every one after it.
        // Outside the permits: nothing else is hashing yet, and a companion
        // has no bean to wait on.
        val dummyHash: String = checkNotNull(
            encoder.encode("this password matches nothing, by construction")
        )
    }
}
