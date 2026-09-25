package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.loginBody
import io.kotest.matchers.collections.shouldContainExactlyInAnyOrder
import io.kotest.matchers.doubles.shouldBeLessThan
import io.kotest.matchers.shouldBe
import org.springframework.http.HttpHeaders
import org.springframework.http.MediaType
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import java.time.Instant
import kotlin.math.abs

// #14 tests 28-34. The backoff is BOOTSTRAP §5.1's "load-bearing departure":
// it lives in login_attempts rather than in process memory precisely so two
// backends cannot drift where contract conformance would never see it.
class LoginBackoffSpec : WebDatabaseSpec() {

    private fun login(
        email: String,
        password: String,
        from: String,
        forwardedFor: String? = null,
    ) = mockMvc.perform(
        post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
            .contentType(MediaType.APPLICATION_JSON)
            .content(loginBody(email, password))
            .apply { forwardedFor?.let { header("X-Forwarded-For", it) } }
            .with { it.remoteAddr = from; it }
    ).andReturn()

    // Seconds from now until the row's backoff_until, measured in the database
    // so the comparison never crosses a clock boundary.
    private fun backoffSecondsFor(key: String): Double =
        JdbcClient.create(dataSource)
            .sql("SELECT extract(epoch FROM (backoff_until - now())) FROM login_attempts WHERE key = :key")
            .param("key", key)
            .query(Double::class.java)
            .single()

    private fun keys(): List<String?> =
        JdbcClient.create(dataSource).sql("SELECT key FROM login_attempts ORDER BY key")
            .query(String::class.java).list()

    init {
        // firstFailureSetsBackoffToOneSecond
        "sets a one-second backoff on the first failure" {
            AuthFixtures.account(dataSource)

            login("user@example.com", "wrong password", "198.51.100.30").response.status shouldBe 401

            JdbcClient.create(dataSource)
                .sql("SELECT failure_count FROM login_attempts WHERE key = 'email:user@example.com'")
                .query(Int::class.java).single() shouldBe 1
            abs(backoffSecondsFor("email:user@example.com") - 1.0) shouldBeLessThan 1.0
        }

        // The doubling and the cap are LoginBackoffFixtureSpec's, read from
        // contract/testdata/login_backoff.json, which Go's suite reads too (#16).

        "records both the ip and the email key on a failure" {
            AuthFixtures.account(dataSource)

            login("user@example.com", "wrong password", "198.51.100.31")

            // Per-IP alone lets an attacker spread across addresses; per-email
            // alone lets anyone lock out a known address.
            keys() shouldContainExactlyInAnyOrder listOf<String?>("email:user@example.com", "ip:198.51.100.31")
        }

        "consults the email key even from a fresh address" {
            AuthFixtures.account(dataSource)
            AuthFixtures.loginAttempt(
                dataSource, "email:user@example.com", 5, Instant.now().plusSeconds(120)
            )

            login("user@example.com", AuthFixtures.PASSWORD, "198.51.100.32").response.status shouldBe 429
        }

        "consults the ip key even for a fresh address" {
            AuthFixtures.account(dataSource, email = "other@example.com")
            AuthFixtures.loginAttempt(
                dataSource, "ip:198.51.100.33", 5, Instant.now().plusSeconds(120)
            )

            login("other@example.com", AuthFixtures.PASSWORD, "198.51.100.33").response.status shouldBe 429
        }

        // backoffReturns429WithRetryAfterInSeconds
        "answers 429 with Retry-After in whole seconds, rounded up" {
            AuthFixtures.account(dataSource)
            AuthFixtures.loginAttempt(
                dataSource, "ip:198.51.100.34", 5, Instant.now().plusMillis(42_500)
            )

            val result = login("user@example.com", AuthFixtures.PASSWORD, "198.51.100.34")

            result.response.status shouldBe 429
            result.response.contentAsString shouldBe """{"code":"TOO_MANY_ATTEMPTS"}"""
            // Rounded up, never 0: a Retry-After of 0 invites an immediate
            // retry that is still inside the window. Go computes it the same
            // way (int(wait.Seconds()) + 1), including the same overshoot on a
            // whole number of seconds - parity matters more than the half-second.
            result.response.getHeader(HttpHeaders.RETRY_AFTER) shouldBe "43"
        }

        // backoffIsNeverAHardLockout
        "lets the correct password through once the window has passed" {
            AuthFixtures.account(dataSource)
            // A long history, and an expired window: no number of failures
            // makes a household's own data permanently unreachable.
            AuthFixtures.loginAttempt(
                dataSource, "email:user@example.com", 50, Instant.now().minusSeconds(1)
            )
            AuthFixtures.loginAttempt(
                dataSource, "ip:198.51.100.35", 50, Instant.now().minusSeconds(1)
            )

            login("user@example.com", AuthFixtures.PASSWORD, "198.51.100.35").response.status shouldBe 204
        }

        // backoffStateSurvivesARestart
        "throttles from state it never saw written" {
            AuthFixtures.account(dataSource)
            // Nothing in this process has failed a login for this key: the row
            // was put there directly, exactly as a previous process would have
            // left it. An in-memory limiter would answer 204 here, and two
            // backends with in-memory limiters diverge invisibly.
            AuthFixtures.loginAttempt(
                dataSource, "ip:198.51.100.36", 3, Instant.now().plusSeconds(120)
            )

            login("user@example.com", AuthFixtures.PASSWORD, "198.51.100.36").response.status shouldBe 429
        }

        // rateLimitKeyIsTheConnectionAddressNeverXForwardedFor
        "keys on the connection address and ignores X-Forwarded-For" {
            AuthFixtures.account(dataSource)

            login(
                "user@example.com",
                "wrong password",
                from = "198.51.100.37",
                forwardedFor = "203.0.113.77",
            ).response.status shouldBe 401

            // Self-hosting means there may be no proxy stripping the header, so
            // it is attacker-controlled: honouring it would let an attacker
            // pick a fresh key per request and the backoff would stop existing.
            keys() shouldContainExactlyInAnyOrder listOf<String?>("email:user@example.com", "ip:198.51.100.37")
        }

        "does not honour X-Forwarded-For to escape an active window" {
            AuthFixtures.account(dataSource)
            AuthFixtures.loginAttempt(
                dataSource, "ip:198.51.100.38", 3, Instant.now().plusSeconds(120)
            )

            login(
                "user@example.com",
                AuthFixtures.PASSWORD,
                from = "198.51.100.38",
                forwardedFor = "203.0.113.78",
            ).response.status shouldBe 429
        }
    }
}
