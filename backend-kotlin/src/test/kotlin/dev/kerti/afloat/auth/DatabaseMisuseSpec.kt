package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.auth.data.CredentialRepository
import dev.kerti.afloat.auth.data.LoginAttemptRepository
import dev.kerti.afloat.auth.data.Session
import dev.kerti.afloat.auth.data.SessionRepository
import dev.kerti.afloat.auth.data.UserRepository
import dev.kerti.afloat.config.AppConfig
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.loggedBy
import dev.kerti.afloat.testsupport.loginBody
import dev.kerti.afloat.testsupport.sha256Hex
import io.kotest.assertions.throwables.shouldThrowAny
import io.kotest.matchers.collections.shouldContain
import io.kotest.matchers.collections.shouldNotContain
import io.kotest.matchers.shouldBe
import jakarta.servlet.http.Cookie
import org.mockito.ArgumentMatchers.any
import org.mockito.ArgumentMatchers.anyCollection
import org.mockito.ArgumentMatchers.anyDouble
import org.mockito.ArgumentMatchers.anyLong
import org.mockito.ArgumentMatchers.anyString
import org.mockito.Mockito.doThrow
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.http.MediaType
import org.springframework.test.context.bean.override.mockito.MockitoSpyBean
import org.springframework.test.web.servlet.MvcResult
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import org.springframework.transaction.IllegalTransactionStateException
import java.time.Instant
import java.util.UUID

// The other half of every isDatabaseFailure() catch: anything it does not
// count goes past it. DatabaseFailureSpec pins the predicate; this pins that
// each catch asks it, so misuse fails loudly instead of being answered as an
// outage. Before this spec, deleting any one catch's rethrow passed every test.
//
// SessionRepository is spied, so no login here can succeed (see
// SessionIssueFailureSpec); none needs to, as each fails before saveAndFlush
// or at it.
class DatabaseMisuseSpec : WebDatabaseSpec() {

    @Autowired
    private lateinit var appConfig: AppConfig

    @MockitoSpyBean
    private lateinit var userRepository: UserRepository

    @MockitoSpyBean
    private lateinit var credentialRepository: CredentialRepository

    @MockitoSpyBean
    private lateinit var loginAttemptRepository: LoginAttemptRepository

    @MockitoSpyBean
    private lateinit var sessionRepository: SessionRepository

    private val misuse = IllegalTransactionStateException("misuse")

    // Mockito's any() returns null, which a Kotlin non-null parameter rejects
    // (same trick as SessionTouchFailureSpec.anyInstant).
    private fun anyUuid(): UUID = any(UUID::class.java) ?: UUID(0, 0)
    private fun anyInstant(): Instant = any(Instant::class.java) ?: Instant.EPOCH

    private fun login(email: String, password: String = AuthFixtures.PASSWORD) = mockMvc.perform(
        post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
            .contentType(MediaType.APPLICATION_JSON)
            .content(loginBody(email, password))
    ).andReturn()

    private fun liveSession(expiresAt: Instant = Instant.now().plusSeconds(3600)): String {
        val account = AuthFixtures.account(dataSource)
        val token = UUID.randomUUID().toString()
        AuthFixtures.session(dataSource, account.userId, sha256Hex(token), expiresAt = expiresAt)
        return token
    }

    // An explicit catch and the catch-all answer with the same 500, so for
    // these the log is what shows the explicit one let the exception by.
    private fun answers500Without(message: String, request: () -> MvcResult) {
        val (result, logged) = loggedBy(AuthService::class.java, request)
        result.response.status shouldBe 500
        result.response.contentAsString shouldBe """{"code":"INTERNAL"}"""
        logged shouldNotContain message
    }

    // SessionFilter's catches serve the request unauthenticated; one that let
    // misuse by ends the request with it instead.
    private fun escapesSessionFilter(token: String) {
        val thrown = shouldThrowAny {
            mockMvc.perform(
                get(AuthApi.BASE_PATH + AuthApi.PATH_GET_ME)
                    .cookie(Cookie(SessionCookieFactory.COOKIE_NAME, token))
            )
        }
        generateSequence(thrown) { it.cause }.toList() shouldContain misuse
    }

    init {
        "lets misuse past the user lookup's catch" {
            val account = AuthFixtures.account(dataSource)
            doThrow(misuse).`when`(userRepository).findByEmail(anyString())

            answers500Without("login: look up user") { login(account.email) }
        }

        "lets misuse past the credential lookup's catch" {
            val account = AuthFixtures.account(dataSource)
            doThrow(misuse).`when`(credentialRepository).findByUserId(account.userId)

            answers500Without("login: look up credential") { login(account.email) }
        }

        "lets misuse past the backoff read's catch" {
            val account = AuthFixtures.account(dataSource)
            doThrow(misuse).`when`(loginAttemptRepository).activeBackoffSeconds(anyCollection())

            answers500Without("login: read backoff") { login(account.email) }
        }

        // Its catch answers 401, so a swallowed misuse would too.
        "lets misuse past recordFailures' catch" {
            val account = AuthFixtures.account(dataSource)
            doThrow(misuse).`when`(loginAttemptRepository).recordFailure(anyString(), anyDouble(), anyDouble())

            answers500Without("login: record failure") { login(account.email, "not the password") }
        }

        // Its catch lets the login go on to issue the session.
        "lets misuse past the clear-attempts catch" {
            val account = AuthFixtures.account(dataSource)
            doThrow(misuse).`when`(loginAttemptRepository).deleteByKeyIn(anyCollection())

            answers500Without("login: clear attempts") { login(account.email) }
        }

        "lets misuse past the issue-session catch" {
            val account = AuthFixtures.account(dataSource)
            doThrow(misuse).`when`(sessionRepository).saveAndFlush(any(Session::class.java))

            answers500Without("login: issue session") { login(account.email) }
        }

        "lets misuse past logout's catch" {
            val token = liveSession()
            doThrow(misuse).`when`(sessionRepository).deleteRow(anyString())

            answers500Without("logout: delete session") {
                mockMvc.perform(
                    post(AuthApi.BASE_PATH + AuthApi.PATH_LOGOUT)
                        .cookie(Cookie(SessionCookieFactory.COOKIE_NAME, token))
                ).andReturn()
            }
        }

        "lets misuse past the session lookup's catch" {
            val token = liveSession()
            doThrow(misuse).`when`(sessionRepository).findLive(anyString(), anyLong())

            escapesSessionFilter(token)
        }

        "lets misuse past the session user lookup's catch" {
            val token = liveSession()
            doThrow(misuse).`when`(userRepository).findById(anyUuid())

            escapesSessionFilter(token)
        }

        "lets misuse past the session refresh's catch" {
            // Past half the TTL, so the filter will try to refresh.
            val token = liveSession(Instant.now().plus(appConfig.sessionTtl.dividedBy(2)).minusSeconds(60))
            doThrow(misuse).`when`(sessionRepository).touch(anyString(), anyInstant())

            escapesSessionFilter(token)
        }
    }
}
