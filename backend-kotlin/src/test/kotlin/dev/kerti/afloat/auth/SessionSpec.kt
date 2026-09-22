package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.api.SystemApi
import dev.kerti.afloat.config.AppConfig
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.cookieValue
import dev.kerti.afloat.testsupport.sessionCookieHeader
import dev.kerti.afloat.testsupport.sha256Hex
import io.kotest.matchers.nulls.shouldBeNull
import io.kotest.matchers.nulls.shouldNotBeNull
import io.kotest.matchers.shouldBe
import io.kotest.matchers.shouldNotBe
import io.kotest.matchers.string.shouldContain
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.web.servlet.MvcResult
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get
import java.time.Instant
import java.util.UUID

// #14 tests 36-45. Two lifetimes, both enforced in SQL, and a refresh that
// only happens past half the TTL - the departures from Balances, where a
// stolen cookie lives forever and every GET is a write.
class SessionSpec : WebDatabaseSpec() {

    @Autowired
    private lateinit var appConfig: AppConfig

    private val mePath = AuthApi.BASE_PATH + AuthApi.PATH_GET_ME

    private fun getMe(token: String?, userAgent: String? = null): MvcResult =
        mockMvc.perform(
            get(mePath)
                .apply { token?.let { cookie(jakarta.servlet.http.Cookie(SessionCookieFactory.COOKIE_NAME, it)) } }
                .apply { userAgent?.let { header("User-Agent", it) } }
        ).andReturn()

    // A plaintext token the way a browser would hold one; only its SHA-256
    // reaches the table.
    private fun seedSession(
        userId: UUID,
        createdAt: Instant = Instant.now(),
        expiresAt: Instant = Instant.now().plus(appConfig.sessionTtl),
        lastSeenAt: Instant = Instant.now(),
        userAgent: String? = null,
    ): String {
        val token = UUID.randomUUID().toString()
        AuthFixtures.session(dataSource, userId, sha256Hex(token), createdAt, expiresAt, lastSeenAt, userAgent)
        return token
    }

    private fun sessionRow(token: String): Map<String, Any?>? =
        JdbcClient.create(dataSource)
            .sql("SELECT expires_at, last_seen_at, user_agent FROM sessions WHERE id = :id")
            .param("id", sha256Hex(token))
            .query().listOfRows().firstOrNull()

    // The cookie the server sends to stop a browser presenting a token it
    // cannot honour: an empty value with an immediate expiry.
    private fun MvcResult.clearsTheCookie(): Boolean {
        val header = sessionCookieHeader() ?: return false
        return header.cookieValue().isEmpty() && header.contains("Max-Age=0")
    }

    init {
        // validSessionCookieResolvesToTheUser
        "resolves a valid session cookie to its user" {
            val account = AuthFixtures.account(dataSource)
            val token = seedSession(account.userId)

            val result = getMe(token)

            result.response.status shouldBe 200
            result.response.contentAsString shouldContain account.userId.toString()
        }

        // expiredSessionIsRejectedAndTheCookieIsCleared
        "rejects an expired session and clears the cookie" {
            val account = AuthFixtures.account(dataSource)
            val token = seedSession(
                account.userId,
                createdAt = Instant.now().minusSeconds(7200),
                expiresAt = Instant.now().minusSeconds(1),
            )

            val result = getMe(token)

            result.response.status shouldBe 401
            result.clearsTheCookie() shouldBe true
        }

        // sessionPastAbsoluteMaxLifetimeIsRejectedEvenIfRecentlyUsed
        "rejects a session past its absolute lifetime however recently it was used" {
            val account = AuthFixtures.account(dataSource)
            // The departure from Balances: continued use there keeps a stolen
            // cookie alive indefinitely and the cap never arrives. Here the
            // sliding window is wide open and the row still cannot be honoured.
            val token = seedSession(
                account.userId,
                createdAt = Instant.now().minus(appConfig.sessionMaxLifetime).minusSeconds(60),
                expiresAt = Instant.now().plus(appConfig.sessionTtl),
                lastSeenAt = Instant.now(),
            )

            val result = getMe(token)

            result.response.status shouldBe 401
            result.clearsTheCookie() shouldBe true
        }

        // unknownOrRevokedSessionCookieIsClearedFromTheBrowser
        "clears a cookie the server cannot honour" {
            AuthFixtures.account(dataSource)

            val result = getMe("a token that was never issued")

            result.response.status shouldBe 401
            result.clearsTheCookie() shouldBe true
        }

        // sessionForASoftDeletedUserIsRejectedAndCleared
        "rejects and clears a session whose user was soft-deleted" {
            val account = AuthFixtures.account(dataSource, userDeletedAt = Instant.now().minusSeconds(60))
            val token = seedSession(account.userId)

            val result = getMe(token)

            result.response.status shouldBe 401
            result.clearsTheCookie() shouldBe true
        }

        // sessionIsNotTouchedWhileMoreThanHalfTheTtlRemains
        "does not write while more than half the TTL remains" {
            val account = AuthFixtures.account(dataSource)
            val token = seedSession(account.userId)
            val before = sessionRow(token)

            val result = getMe(token)

            result.response.status shouldBe 200
            // Capture is the hot path and the PWA re-probes on resume, so a
            // write per authenticated request would make every GET a write on
            // the one table every request reads.
            sessionRow(token) shouldBe before
            result.sessionCookieHeader().shouldBeNull()
        }

        // sessionIsTouchedOncePastHalfTheTtl
        "extends the window once past half the TTL, carrying the plaintext token" {
            val account = AuthFixtures.account(dataSource)
            val token = seedSession(
                account.userId,
                // Just inside the refresh threshold.
                expiresAt = Instant.now().plus(appConfig.sessionTtl.dividedBy(2)).minusSeconds(60),
            )
            val before = sessionRow(token)

            val result = getMe(token)

            result.response.status shouldBe 200
            val after = sessionRow(token)
            after.shouldNotBeNull()
            after["expires_at"] shouldNotBe before!!["expires_at"]
            after["last_seen_at"] shouldNotBe before["last_seen_at"]

            // The single most likely bug in the whole issue: writing the HASH
            // into the refreshed cookie means the next lookup never matches.
            val refreshed = result.sessionCookieHeader()
            refreshed.shouldNotBeNull()
            refreshed.cookieValue() shouldBe token

            // And the refreshed cookie still works.
            getMe(refreshed.cookieValue()).response.status shouldBe 200
        }

        // sessionMiddlewareDoesNotRejectUnauthenticatedRequests
        "leaves a public route reachable with a garbage cookie present" {
            // Resolution and enforcement are separate concerns: the filter
            // resolves, the authorization rules reject.
            val result = mockMvc.perform(
                get(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH)
                    .cookie(jakarta.servlet.http.Cookie(SessionCookieFactory.COOKIE_NAME, "garbage"))
            ).andReturn()

            result.response.status shouldBe 200
        }

        // userAgentIsRecordedOnTheSessionRow
        "records the user agent on the session row and nothing else about the request" {
            AuthFixtures.account(dataSource)

            mockMvc.perform(
                org.springframework.test.web.servlet.request.MockMvcRequestBuilders
                    .post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
                    .contentType(org.springframework.http.MediaType.APPLICATION_JSON)
                    .content(dev.kerti.afloat.testsupport.loginBody("user@example.com", AuthFixtures.PASSWORD))
                    .header("User-Agent", "afloat-pwa/1.0")
                    .with { it.remoteAddr = "198.51.100.40"; it }
            ).andReturn().response.status shouldBe 204

            val row = JdbcClient.create(dataSource).sql("SELECT * FROM sessions").query().singleRow()
            row["user_agent"] shouldBe "afloat-pwa/1.0"
            // PRD N6: not the IP, not a fingerprint. The table has no column
            // for either, and this is what keeps it that way.
            row.keys.map { it.lowercase() }.toSet() shouldBe
                setOf("id", "user_id", "created_at", "expires_at", "last_seen_at", "user_agent")
            row.values.none { it?.toString() == "198.51.100.40" } shouldBe true
        }
    }
}
