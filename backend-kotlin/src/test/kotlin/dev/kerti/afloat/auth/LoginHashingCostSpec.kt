package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.loginBody
import io.kotest.matchers.shouldBe
import org.mockito.Mockito.never
import org.mockito.Mockito.verify
import org.mockito.ArgumentMatchers.anyString
import org.springframework.http.MediaType
import org.springframework.test.context.bean.override.mockito.MockitoSpyBean
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import java.time.Instant

// #14 tests 23 and 35: what the login actually SPENDS. Asserted through a spy
// on PasswordService rather than a wall clock - a timing assertion is flaky by
// construction and would be the first test anyone deletes.
class LoginHashingCostSpec : WebDatabaseSpec() {

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
        // unknownEmailStillPaysTheFullHashingCost
        "verifies against the dummy hash for an address with no account" {
            val result = login("nobody@example.com", AuthFixtures.PASSWORD, "198.51.100.20")

            result.response.status shouldBe 401
            // The work happens, and it happens against a hash with the mandated
            // parameters - so an unknown address cannot be told from a real one
            // by how long the answer took.
            verify(passwordService).verify(AuthFixtures.PASSWORD, PasswordService.dummyHash)
        }

        // The dormant User: invited, owns data, never set a password. Same cost.
        "verifies against the dummy hash for a user holding no credential" {
            AuthFixtures.account(dataSource, email = "dormant@example.com", withCredential = false)

            val result = login("dormant@example.com", AuthFixtures.PASSWORD, "198.51.100.21")

            result.response.status shouldBe 401
            verify(passwordService).verify(AuthFixtures.PASSWORD, PasswordService.dummyHash)
        }

        // backoffIsCheckedBeforeCredentialsAreVerified
        "does no hashing at all for a request inside the backoff window" {
            AuthFixtures.account(dataSource)
            AuthFixtures.loginAttempt(
                dataSource,
                "ip:198.51.100.22",
                failureCount = 4,
                backoffUntil = Instant.now().plusSeconds(120),
            )

            val result = login("user@example.com", AuthFixtures.PASSWORD, "198.51.100.22")

            result.response.status shouldBe 429
            // A throttled request must be cheap, or the backoff becomes the
            // amplifier it was meant to prevent.
            verify(passwordService, never()).verify(anyString(), anyString())
        }
    }
}
