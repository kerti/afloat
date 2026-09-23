package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.api.SystemApi
import dev.kerti.afloat.auth.data.SessionRepository
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.jsonFields
import dev.kerti.afloat.testsupport.loginBody
import io.kotest.matchers.shouldBe
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.http.MediaType
import org.springframework.test.context.TestPropertySource
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post

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
