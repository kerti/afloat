package dev.kerti.afloat.security

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.api.SystemApi
import dev.kerti.afloat.auth.SessionCookieFactory
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.loginBody
import dev.kerti.afloat.testsupport.sha256Hex
import io.kotest.assertions.withClue
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldNotContain
import jakarta.servlet.http.Cookie
import org.springframework.http.MediaType
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.web.servlet.MvcResult
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import java.time.Instant
import java.util.UUID

// #14 tests 68-72: #13's permit-all chain, tightened. Public by exception and
// authenticated by default, so a new endpoint in the contract cannot ship open
// because nobody remembered a matcher.
class SecurityChainSpec : WebDatabaseSpec() {

    private fun countOf(table: String): Long =
        JdbcClient.create(dataSource).sql("SELECT count(*) FROM $table").query(Long::class.java).single()

    private fun MvcResult.issuedNoJsessionid() {
        response.getHeaders("Set-Cookie").forEach { it shouldNotContain "JSESSIONID" }
        // The stateless policy means the container session is never created in
        // the first place, not merely that the cookie is suppressed.
        request.getSession(false) shouldBe null
    }

    init {
        // theGuardRunsBeforeAnyHandler
        "blocks a cross-site login before it reaches the handler or the database" {
            AuthFixtures.account(dataSource)

            val result = mockMvc.perform(
                post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
                    .contentType(MediaType.APPLICATION_JSON)
                    .content(loginBody("user@example.com", AuthFixtures.PASSWORD))
                    .header("Sec-Fetch-Site", "cross-site")
            ).andReturn()

            result.response.status shouldBe 403
            result.response.contentAsString shouldBe """{"code":"CROSS_SITE_REQUEST_BLOCKED"}"""
            // Correct credentials, and still no session: the guard runs before
            // anything that could have issued one.
            countOf("sessions") shouldBe 0L
            countOf("login_attempts") shouldBe 0L
        }

        // protectedRoutesRequireASessionAndReturn401Unauthorized
        // a401CarriesTheContractEnvelopeNotSpringsDefault
        "answers a session-less protected route with the contract's 401 envelope" {
            val result = mockMvc.perform(get(AuthApi.BASE_PATH + AuthApi.PATH_GET_ME)).andReturn()

            result.response.status shouldBe 401
            // Spring Security's entry point writes its own body from a filter,
            // outside controller exception handling. That is the trap: the
            // default is an empty body with a WWW-Authenticate header.
            result.response.contentAsString shouldBe """{"code":"UNAUTHORIZED"}"""
            result.response.contentType!! shouldNotContain "text/html"
            result.response.getHeader("WWW-Authenticate") shouldBe null
        }

        // publicRoutesRemainPublic
        "leaves the public routes public" {
            AuthFixtures.account(dataSource)

            withClue("GET /api/health") {
                mockMvc.perform(get(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH))
                    .andReturn().response.status shouldBe 200
            }
            withClue("GET /api/auth/methods") {
                mockMvc.perform(get(SystemApi.BASE_PATH + SystemApi.PATH_GET_AUTH_METHODS))
                    .andReturn().response.status shouldBe 200
            }
            withClue("POST /api/auth/local/login") {
                mockMvc.perform(
                    post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(loginBody("user@example.com", AuthFixtures.PASSWORD))
                ).andReturn().response.status shouldBe 204
            }
        }

        "answers logout with or without a session" {
            val account = AuthFixtures.account(dataSource)
            val token = UUID.randomUUID().toString()
            AuthFixtures.session(
                dataSource, account.userId, sha256Hex(token), expiresAt = Instant.now().plusSeconds(3600)
            )

            withClue("with a session") {
                mockMvc.perform(
                    post(AuthApi.BASE_PATH + AuthApi.PATH_LOGOUT)
                        .cookie(Cookie(SessionCookieFactory.COOKIE_NAME, token))
                ).andReturn().response.status shouldBe 204
            }
            withClue("without a session") {
                mockMvc.perform(post(AuthApi.BASE_PATH + AuthApi.PATH_LOGOUT))
                    .andReturn().response.status shouldBe 204
            }
        }

        // stillNoJsessionidIsIssued
        "still issues no JSESSIONID" {
            AuthFixtures.account(dataSource)

            // The permit-all chain from #13 is gone; the stateless policy is not.
            mockMvc.perform(get(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH))
                .andReturn().issuedNoJsessionid()
            mockMvc.perform(get(AuthApi.BASE_PATH + AuthApi.PATH_GET_ME))
                .andReturn().issuedNoJsessionid()
            mockMvc.perform(
                post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
                    .contentType(MediaType.APPLICATION_JSON)
                    .content(loginBody("user@example.com", AuthFixtures.PASSWORD))
            ).andReturn().issuedNoJsessionid()
        }

        // The actuator is deliberately not public: it is not part of the
        // contract, and /api/health is the liveness route both backends serve.
        "keeps the actuator behind authentication" {
            mockMvc.perform(get("/actuator/health")).andReturn().response.status shouldBe 401
        }
    }
}
