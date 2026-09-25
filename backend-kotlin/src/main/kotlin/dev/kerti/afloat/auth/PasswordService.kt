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

// Thrown when no Argon2 permit came free within the wait, or the wait was
// interrupted. The password was never checked, so this is neither a match nor
// a mismatch.
class HashingUnavailableException(cause: Throwable? = null) :
    RuntimeException("no Argon2 permit within the wait", cause)

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

    // Callers queued for a permit, so a test can act once one is queued rather
    // than after a guessed delay.
    internal val queuedForPermit: Int get() = permits.queueLength

    fun hash(password: String): String = withPermit { checkNotNull(encoder.encode(password)) }

    // Checks a password against a stored PHC string. The caller must already
    // hold a permit, and may go on holding it past the hash: login keeps its
    // permit until a failure is recorded (AuthService.login). Never call it
    // without one.
    //
    // A corrupt row must fail the login, not crash the handler. The decoder
    // throws whatever the malformation happens to produce - IllegalArgument for
    // a bad number, ArrayIndexOutOfBounds for missing segments,
    // UnsupportedOperation for an algorithm or version it does not implement -
    // so the catch is by outcome, not by exception type. Go's verifyHoldingPermit
    // returns a bool for the same reason.
    //
    // The shape is checked first, because the decoder is looser than Go's
    // parsePHC: it ignores a sixth field and accepts base64 padding, so a string
    // Go refused verified here (contract/testdata/argon2.json, #16).
    internal fun verifyHoldingPermit(password: String, phc: String): Boolean = try {
        hasPhcShape(phc) && encoder.matches(password, phc)
    } catch (e: RuntimeException) {
        log.warn("password verify: unusable stored hash", e)
        false
    }

    internal fun <T> withPermit(block: () -> T): T {
        val acquired = try {
            permits.tryAcquire(permitWait.toNanos(), TimeUnit.NANOSECONDS)
        } catch (e: InterruptedException) {
            // Whoever interrupted still needs to see it, and the caller gets
            // the permit-wait answer, not the catch-all's.
            Thread.currentThread().interrupt()
            throw HashingUnavailableException(e)
        }
        if (!acquired) throw HashingUnavailableException()
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

        // The one PHC spelling both backends accept, Go's phcShape: argon2id,
        // version 19, the three parameters in that order, unpadded standard
        // base64, a hash of at least 4 bytes, 1..255 lanes (Go reads the lane
        // count into a byte), 8 KiB per lane to MAX_MEMORY_KIB, and 1 to
        // MAX_TIME iterations. Below the memory floor both libraries quietly
        // raise memory to it, so the string no longer states the work done,
        // and at m=0 only this decoder refuses. Iterations are read as an Int
        // where Go reads a uint32, well within MAX_TIME. Each bound is held
        // here, and so is a base64 length that cannot decode, so that nothing
        // this refuses reaches the decoder, which throws and logs where Go
        // refuses quietly.
        private val PHC_SHAPE =
            Regex("""^[$]argon2id[$]v=19[$]m=(\d+),t=(\d+),p=(\d+)[$]([A-Za-z0-9+/]+)[$]([A-Za-z0-9+/]{6,})$""")

        // MAX_MEMORY_KIB and MAX_TIME are the accepted ceiling for a STORED
        // hash's m and t (#68, BOOTSTRAP.md §5.1) - headroom over the
        // operating cost (m=19456 KiB, t=2), not BouncyCastle's own 2^24 KiB
        // argon2.max_memory_exp ceiling. A cost increase past either ceiling
        // means raising the ceiling first, deliberately, rather than silently
        // accepting whatever a stored string names.
        private const val MAX_MEMORY_KIB = 65536 // 64 MiB, matching Go's argonMaxMemory.
        private const val MAX_TIME = 10

        internal fun hasPhcShape(phc: String): Boolean {
            val (m, t, p, salt, hash) = PHC_SHAPE.matchEntire(phc)?.destructured ?: return false
            val memory = m.toIntOrNull() ?: return false
            val iterations = t.toIntOrNull() ?: return false
            val lanes = p.toIntOrNull() ?: return false
            return iterations in 1..MAX_TIME && lanes in 1..255 && memory in 8 * lanes..MAX_MEMORY_KIB &&
                salt.length % 4 != 1 && hash.length % 4 != 1
        }

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
