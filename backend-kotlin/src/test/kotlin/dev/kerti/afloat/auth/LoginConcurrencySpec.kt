package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.loginBody
import io.kotest.assertions.withClue
import io.kotest.matchers.collections.shouldBeEmpty
import io.kotest.matchers.longs.shouldBeLessThan
import io.kotest.matchers.shouldBe
import org.mockito.ArgumentMatchers.anyString
import org.mockito.Mockito.doAnswer
import org.mockito.Mockito.doThrow
import org.springframework.http.MediaType
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.context.bean.override.mockito.MockitoSpyBean
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import org.springframework.transaction.support.TransactionSynchronizationManager
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

// The login hashes for tens of milliseconds, and a transaction spanning that
// would pin a pooled connection for all of it. "holds no transaction while it
// hashes" is the guard against that one: made @Transactional, login still
// clears the burst in seconds, because recordFailure joins the caller's
// transaction rather than taking a second connection. The burst guards the
// rest: bad logins wider than the pool, from an unauthenticated caller, each
// reaching the hash and the failure write, all answer 401 without stalling.
class LoginConcurrencySpec : WebDatabaseSpec() {

    @MockitoSpyBean
    private lateinit var passwordService: PasswordService

    private fun login(email: String, password: String, from: String) =
        mockMvc.perform(
            post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
                .contentType(MediaType.APPLICATION_JSON)
                .content(loginBody(email, password))
                .with { it.remoteAddr = from; it }
        ).andReturn()

    init {
        // Past Hikari's default of 10, so a connection held per request cannot
        // all be served at once, and three times the Argon2 cap. No wider: the
        // cap runs the hashes four at a time, so twelve callers take three
        // hashes' time end to end, which must stay well inside the bound below
        // on a loaded machine (#33).
        "answers a burst of failed logins wider than the pool without stalling" {
            val callers = 12
            // Half against a real account, half an unknown address, so the
            // real hash and the dummy hash have to share the one cap.
            (2..callers step 2).forEach { AuthFixtures.account(dataSource, email = "known$it@example.com") }
            val ready = CountDownLatch(callers)
            val go = CountDownLatch(1)
            val pool = Executors.newFixedThreadPool(callers)
            passwordService.resetPeakInFlight()
            try {
                val started = System.nanoTime()
                val statuses = (1..callers).map { n ->
                    pool.submit<Int> {
                        ready.countDown()
                        go.await()
                        // Distinct email and address: no backoff row is shared,
                        // so every one of them reaches the hash and the write.
                        val email = if (n % 2 == 0) "known$n@example.com" else "nobody$n@example.com"
                        login(email, "wrong password", "198.51.100.$n").response.status
                    }
                }
                ready.await()
                go.countDown()
                val results = statuses.map { it.get(60, TimeUnit.SECONDS) }
                val elapsedSeconds = TimeUnit.NANOSECONDS.toSeconds(System.nanoTime() - started)

                withClue("statuses: $results") { results.toSet() shouldBe setOf(401) }
                // Deadlocked, every request waits out the 30 s connection timeout.
                elapsedSeconds shouldBeLessThan 20L
                // #33: twelve hashes in flight at once want ~230 MiB of heap.
                passwordService.peakInFlight shouldBe PasswordService.ARGON_CONCURRENCY_CAP
            } finally {
                pool.shutdownNow()
            }
        }

        "holds no transaction while it hashes" {
            AuthFixtures.account(dataSource)
            val inTransaction = AtomicBoolean(true)
            doAnswer { invocation ->
                inTransaction.set(TransactionSynchronizationManager.isActualTransactionActive())
                invocation.callRealMethod()
            }.`when`(passwordService).verify(anyString(), anyString())

            login("user@example.com", "wrong password", "198.51.100.200").response.status shouldBe 401

            inTransaction.get() shouldBe false
        }

        // Queued past the wait for a permit, the password was never checked:
        // not a 401, and not a failure for the backoff to count.
        "answers 500 and records no failure when no hashing permit comes free" {
            AuthFixtures.account(dataSource)
            doThrow(HashingUnavailableException()).`when`(passwordService).verify(anyString(), anyString())

            val result = login("user@example.com", "wrong password", "198.51.100.201")

            result.response.status shouldBe 500
            result.response.contentAsString shouldBe """{"code":"INTERNAL"}"""
            JdbcClient.create(dataSource).sql("SELECT key FROM login_attempts")
                .query(String::class.java).list().shouldBeEmpty()
        }
    }
}
