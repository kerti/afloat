package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.api.SystemApi
import dev.kerti.afloat.auth.data.SessionRepository
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.jsonFields
import dev.kerti.afloat.testsupport.loginBody
import io.kotest.assertions.withClue
import io.kotest.matchers.shouldBe
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.http.MediaType
import org.springframework.test.context.TestPropertySource
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.options
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import java.net.URI

@TestPropertySource(
    properties = [
        "afloat.auth-local-enabled=false",
        "afloat.auth-google-enabled=true",
    ]
)
class LoginProviderDisabledSpec : WebDatabaseSpec() {

    @Autowired
    private lateinit var sessionRepository: SessionRepository

    private val loginPath = AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN

    init {
        "with the local login disabled, POST $loginPath is a bare 404" {
            val rec = mockMvc.perform(
                post(loginPath)
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("""{"email":"a@example.com","password":"whatever"}""")
            ).andReturn()

            rec.response.status shouldBe 404
            rec.response.contentAsString shouldBe ""
            rec.response.getHeader("Content-Type") shouldBe null
        }

        "...and GET is a bare 404 too — method-agnostic, not a 405" {
            val rec = mockMvc.perform(
                get(loginPath)
            ).andReturn()

            rec.response.status shouldBe 404
            rec.response.contentAsString shouldBe ""
            rec.response.getHeader("Content-Type") shouldBe null
        }

        "...byte-for-byte identical to a genuinely-unregistered path" {
            val rec = mockMvc.perform(
                post(loginPath)
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("""{"email":"a@example.com","password":"whatever"}""")
            ).andReturn()

            val unregistered = mockMvc.perform(
                post("/some-random-path-that-we-dont-have")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("""{"email":"a@example.com","password":"whatever"}""")
            ).andReturn()

            rec.response.status shouldBe unregistered.response.status
            rec.response.contentAsString shouldBe unregistered.response.contentAsString
            rec.response.getHeader("Content-Type") shouldBe unregistered.response.getHeader("Content-Type")
        }

        "...no session row is written" {
            val account = AuthFixtures.account(dataSource)

            val rec = mockMvc.perform(
                post(loginPath)
                    .contentType(MediaType.APPLICATION_JSON)
                    .content(loginBody(account.email, AuthFixtures.PASSWORD))
            ).andReturn()

            rec.response.status shouldBe 404
            sessionRepository.count() shouldBe 0
        }

        // An encoded spelling of the path is the path (#56), so the gate holds
        // on every one. Compared against the raw URI it did not: these signed
        // the account in with the provider off.
        "...on every percent-encoded spelling of the path, with no session written" {
            val account = AuthFixtures.account(dataSource)
            listOf("/api/auth/local/%6Cogin", "/api/auth/local/l%6Fgin", "/%61pi/auth/local/login").forEach { path ->
                val rec = mockMvc.perform(
                    post(URI.create(path))
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(loginBody(account.email, AuthFixtures.PASSWORD))
                ).andReturn()

                withClue(path) {
                    rec.response.status shouldBe 404
                    rec.response.contentAsString shouldBe ""
                    sessionRepository.count() shouldBe 0
                }
            }
        }

        // #66's second-review finding: with a method-scoped gate, OPTIONS
        // would answer with an Allow header naming the route's methods,
        // disclosing it exists even though every other method is a bare 404.
        // OptionsRefusedFilter runs ahead of DisabledLocalLogin404Filter, so
        // this is one answer, not two gates that could disagree.
        "...and OPTIONS is the same flat 405, no Allow, as any other path" {
            val rec = mockMvc.perform(options(loginPath)).andReturn()
            val unregistered = mockMvc.perform(options("/some-random-path-that-we-dont-have")).andReturn()

            rec.response.status shouldBe 405
            rec.response.getHeader("Allow") shouldBe null
            rec.response.status shouldBe unregistered.response.status
            rec.response.getHeader("Allow") shouldBe unregistered.response.getHeader("Allow")
        }

        // S1, on the local-disabled profile too: a path the firewall refuses
        // answers OPTIONS the same flat 405 as any other OPTIONS, with local
        // login off as well as on (ErrorDispatchSpec covers it on). This gate
        // being off must not change what a firewall-refused OPTIONS answers.
        "...and OPTIONS on a firewall-refused path is the same flat 405 too" {
            val baseline = mockMvc.perform(options("/api/health")).andReturn()
            baseline.response.status shouldBe 405

            listOf("/api/health;x=1", "/api//health", "/api/%2e%2e/health", "/api/auth%2Fmethods").forEach { path ->
                withClue(path) {
                    val refused = mockMvc.perform(options(URI.create(path))).andReturn()
                    refused.response.status shouldBe 405
                    refused.response.getHeader("Allow") shouldBe null
                    refused.response.contentAsString shouldBe ""
                }
            }
        }

        "...and /auth/methods still reports the gate's flag (local: false)" {
            val rec = mockMvc.perform(
                get(SystemApi.BASE_PATH + SystemApi.PATH_GET_AUTH_METHODS)
            ).andReturn()

            rec.response.status shouldBe 200
            rec.response.contentAsString.jsonFields() shouldBe mapOf(
                "local" to "false",
                "google" to "true",
            )
        }
    }

}
