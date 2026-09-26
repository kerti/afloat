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
import io.kotest.matchers.shouldNotBe
import io.kotest.matchers.string.shouldNotContain
import jakarta.servlet.http.Cookie
import org.springframework.http.MediaType
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.web.servlet.MvcResult
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.head
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.options
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.put
import java.time.Instant
import java.util.UUID

// #14 tests 68-72: #13's permit-all chain, tightened. Public by exception and
// authenticated by default, so a new endpoint in the contract cannot ship open
// because nobody remembered a matcher.
class SecurityChainSpec : WebDatabaseSpec() {

    private fun countOf(table: String): Long =
        JdbcClient.create(dataSource).sql("SELECT count(*) FROM $table").query(Long::class.java).single()

    // Spring Security's own default for an unauthenticated request is a 302 to
    // a generated /login page. A test that only asserted "not 401" would read
    // that redirect as success, so the absence is checked explicitly.
    private fun MvcResult.servedWithoutALoginPage(expectedStatus: Int) {
        response.status shouldBe expectedStatus
        response.getHeader("Location") shouldBe null
    }

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
        // permitAllChainDoesNotServeAGeneratedLoginPage
        //
        // 200 AND no redirect: Spring Security's default for an unauthenticated
        // request is a 302 to a generated /login page, which would read as
        // "reachable" to a test that only checked for a non-401.
        "leaves the public routes public" {
            AuthFixtures.account(dataSource)

            withClue("GET /api/health") {
                mockMvc.perform(get(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH))
                    .andReturn().servedWithoutALoginPage(200)
            }
            withClue("GET /api/auth/methods") {
                mockMvc.perform(get(SystemApi.BASE_PATH + SystemApi.PATH_GET_AUTH_METHODS))
                    .andReturn().servedWithoutALoginPage(200)
            }
            withClue("POST /api/auth/local/login") {
                mockMvc.perform(
                    post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(loginBody("user@example.com", AuthFixtures.PASSWORD))
                ).andReturn().servedWithoutALoginPage(204)
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
        // sessionCreationPolicyIsStateless
        //
        // Afloat's sessions are its own rows in `sessions`, not a servlet
        // container's. A JSESSIONID alongside afloat_session would be a second,
        // unmanaged session mechanism that nothing in either backend expires.
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

        // The actuator is not exposed over HTTP at all (#66): management.server.port:
        // -1 (application.yaml) leaves no controller mapping it, so it falls
        // through `unmapped` the same way any unregistered path does, to
        // Spring MVC's own "no handler" 404 - never a 401 that would still
        // disclose the actuator exists, with or without a session, and never
        // reachable at /api/actuator/health either, since nothing maps that
        // either. Go never had it (#66's acceptance criteria).
        "answers the actuator as an unregistered path, unauthenticated" {
            val result = mockMvc.perform(get("/actuator/health")).andReturn()
            result.response.status shouldBe 404
            result.response.contentAsString shouldBe ""
        }

        "answers the actuator as an unregistered path, signed in" {
            AuthFixtures.account(dataSource)
            mockMvc.perform(
                post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
                    .contentType(MediaType.APPLICATION_JSON)
                    .content(loginBody("user@example.com", AuthFixtures.PASSWORD))
            ).andReturn().response.status shouldBe 204

            val result = mockMvc.perform(get("/actuator/health")).andReturn()
            result.response.status shouldBe 404
            result.response.contentAsString shouldBe ""
        }

        "answers the actuator under the API base path as an unregistered path too" {
            mockMvc.perform(get(SystemApi.BASE_PATH + "/actuator/health")).andReturn().response.status shouldBe 404
        }

        // #66: RFC 9110 §9.1 requires HEAD of a general-purpose server; Spring
        // already does it for any @GetMapping, with no code of Afloat's own,
        // via the servlet spec's own HttpServlet.doHead() - which MockMvc does
        // not fully emulate (it does not strip the body the way a real
        // container does), so the body-is-empty half of this is the
        // conformance suite's to prove, against the real jar
        // (cases/http-methods.yaml). This still pins that the request reaches
        // the GET handler at all, and gets the same status and Content-Type,
        // behind the full security chain.
        "answers HEAD the same route GET does, status and Content-Type alike" {
            val get = mockMvc.perform(get(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH)).andReturn()
            val headResult = mockMvc.perform(head(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH)).andReturn()

            headResult.response.status shouldBe get.response.status
            headResult.response.getHeader("Content-Type") shouldBe get.response.getHeader("Content-Type")
        }

        // #66: OPTIONS is a flat, content-free 405 everywhere - no Allow header
        // naming the route's methods, on a registered route or an unregistered
        // path alike, since Afloat is same-origin with no CORS and no client
        // sends it.
        "answers OPTIONS with a flat 405 and no Allow header, registered or not" {
            for (path in listOf(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH, "/api/no-such-route")) {
                withClue(path) {
                    val result = mockMvc.perform(options(path)).andReturn()
                    result.response.status shouldBe 405
                    result.response.getHeader("Allow") shouldBe null
                    result.response.contentAsString shouldBe ""
                }
            }
        }

        // The first 405 case (#66's acceptance criterion): a wrong method on a
        // route that exists still answers 405, Allow included per RFC 9110
        // §15.5.6 - only OPTIONS is special-cased to withhold it.
        "answers a wrong method on an existing route with 405" {
            val result = mockMvc.perform(put(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH)).andReturn()
            result.response.status shouldBe 405
            result.response.contentAsString shouldBe ""
            result.response.getHeader("Allow") shouldNotBe null
        }
    }
}
