package dev.kerti.afloat.auth

import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.verifyUnderPermit
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
            // Half the right password against a real hash, half the dummy hash
            // an unknown address pays: both share the one cap.
            val results = (1..callers).map { n ->
                pool.submit<Boolean> {
                    go.await()
                    if (n % 2 == 0) {
                        service.verifyUnderPermit(AuthFixtures.PASSWORD, AuthFixtures.PASSWORD_PHC)
                    } else {
                        service.verifyUnderPermit("wrong password", PasswordService.dummyHash)
                    }
                }
            }
            go.countDown()
            results.map { it.get(60, TimeUnit.SECONDS) } shouldBe (1..callers).map { it % 2 == 0 }
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
                service.verifyUnderPermit("wrong password", PasswordService.dummyHash)
            }
        } finally {
            release.countDown()
            pool.shutdown()
        }
    }

    // An interrupted wait checked no password either, so the caller gets the
    // permit-wait answer rather than the catch-all's, and the interrupt is
    // still there for whoever sent it.
    "throws HashingUnavailableException for an interrupted wait, keeping the interrupt" {
        val service = PasswordService(Duration.ofSeconds(30))
        Thread.currentThread().interrupt()
        try {
            shouldThrow<HashingUnavailableException> {
                service.verifyUnderPermit("wrong password", PasswordService.dummyHash)
            }
        } finally {
            Thread.interrupted() shouldBe true
        }
    }
})
