package dev.kerti.afloat.auth

import io.kotest.assertions.throwables.shouldThrow
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe
import java.time.Duration
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

// #33: every Argon2 hash holds its 19 MiB for its duration, so the number in
// flight is the heap's peak. The bound is asserted on the counter PasswordService
// keeps as each call takes a permit, not on memory or elapsed time. Go's
// TestVerifyPasswordBoundsConcurrentArgon2Calls.
class ArgonConcurrencySpec : StringSpec({

    "never runs more hashes at once than the cap, and a burst reaches it" {
        val service = PasswordService(Duration.ofSeconds(30))
        val callers = 20
        val go = CountDownLatch(1)
        val pool = Executors.newFixedThreadPool(callers)
        try {
            val results = (1..callers).map {
                pool.submit<Boolean> {
                    go.await()
                    service.verify("wrong password", PasswordService.dummyHash)
                }
            }
            go.countDown()
            results.map { it.get(60, TimeUnit.SECONDS) }.toSet() shouldBe setOf(false)
        } finally {
            pool.shutdownNow()
        }

        // Reaching the cap is what shows the burst contended at all.
        service.peakInFlight shouldBe PasswordService.ARGON_CONCURRENCY_CAP
    }

    // Queued past the wait, the password was never checked: an exception,
    // never a false that the caller would score as a wrong password.
    "throws rather than answering false when no permit comes free within the wait" {
        val service = PasswordService(Duration.ofMillis(100))
        val holding = CountDownLatch(PasswordService.ARGON_CONCURRENCY_CAP)
        val release = CountDownLatch(1)
        val pool = Executors.newFixedThreadPool(PasswordService.ARGON_CONCURRENCY_CAP)
        try {
            repeat(PasswordService.ARGON_CONCURRENCY_CAP) {
                pool.submit {
                    service.withPermit {
                        holding.countDown()
                        release.await()
                    }
                }
            }
            holding.await(10, TimeUnit.SECONDS) shouldBe true

            shouldThrow<HashingUnavailableException> {
                service.verify("wrong password", PasswordService.dummyHash)
            }
        } finally {
            release.countDown()
            pool.shutdown()
        }
    }
})
