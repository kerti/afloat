package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.cookieAttributes
import dev.kerti.afloat.testsupport.cookieValue
import dev.kerti.afloat.testsupport.sessionCookieHeader
import dev.kerti.afloat.testsupport.sha256Hex
import io.kotest.matchers.collections.shouldContain
import io.kotest.matchers.collections.shouldNotContain
import io.kotest.matchers.nulls.shouldNotBeNull
import io.kotest.matchers.shouldBe
import jakarta.servlet.http.Cookie
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.web.servlet.MvcResult
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import java.time.Instant
import java.util.UUID

// #14 tests 56-60. Revocation IS the row delete: sessions are instance-local
// auth state and exempt from the soft-delete rule (BOOTSTRAP §4).
class LogoutSpec : WebDatabaseSpec() {

    private val logoutPath = AuthApi.BASE_PATH + AuthApi.PATH_LOGOUT

    private fun seedSessionFor(userId: UUID): String {
        val token = UUID.randomUUID().toString()
        AuthFixtures.session(
            dataSource, userId, sha256Hex(token), expiresAt = Instant.now().plusSeconds(3600)
        )
        return token
    }

    private fun logout(token: String?): MvcResult =
        mockMvc.perform(
            post(logoutPath).apply {
                token?.let { cookie(Cookie(SessionCookieFactory.COOKIE_NAME, it)) }
            }
        ).andReturn()

    private fun sessionIds(): List<String?> =
        JdbcClient.create(dataSource).sql("SELECT id FROM sessions").query(String::class.java).list()

    init {
        // logoutDestroysTheSessionRow
        "destroys the session row" {
            val account = AuthFixtures.account(dataSource)
            val token = seedSessionFor(account.userId)

            logout(token).response.status shouldBe 204

            // Deleted, not soft-deleted: the sessions table has no deleted_at
            // and must not grow one.
            sessionIds() shouldBe emptyList()
        }

        // logoutClearsTheCookie
        "clears the cookie with the same attributes" {
            val account = AuthFixtures.account(dataSource)
            val token = seedSessionFor(account.userId)

            val header = logout(token).sessionCookieHeader()

            header.shouldNotBeNull()
            header.cookieValue() shouldBe ""
            val attributes = header.cookieAttributes()
            attributes["max-age"] shouldBe "0"
            attributes["path"] shouldBe "/"
            attributes.keys shouldContain "httponly"
            attributes["samesite"] shouldBe "Lax"
            attributes.keys shouldNotContain "domain"
        }

        // logoutWithoutASessionReturns204
        // logoutClearsTheCookieEvenWhenThereWasNoSession
        "succeeds and still clears the cookie with no session at all" {
            val result = logout(null)

            // Idempotent by contract, so a client clearing a stale cookie never
            // special-cases the result.
            result.response.status shouldBe 204
            val header = result.sessionCookieHeader()
            header.shouldNotBeNull()
            header.cookieValue() shouldBe ""
        }

        "succeeds for a cookie the server has never heard of" {
            val result = logout("a token that was never issued")

            result.response.status shouldBe 204
            result.sessionCookieHeader()?.cookieValue() shouldBe ""
        }

        // logoutDoesNotAffectTheUsersOtherSessions
        "leaves the user's other sessions alone" {
            val account = AuthFixtures.account(dataSource)
            val phone = seedSessionFor(account.userId)
            val laptop = seedSessionFor(account.userId)

            logout(phone).response.status shouldBe 204

            // One device, not all. Revoking everywhere is a password reset.
            sessionIds() shouldBe listOf(sha256Hex(laptop))
        }
    }
}
