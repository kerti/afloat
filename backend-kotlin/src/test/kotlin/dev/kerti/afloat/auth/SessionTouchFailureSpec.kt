package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.auth.data.SessionRepository
import dev.kerti.afloat.config.AppConfig
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.sha256Hex
import io.kotest.matchers.nulls.shouldBeNull
import io.kotest.matchers.shouldBe
import jakarta.servlet.http.Cookie
import org.mockito.ArgumentMatchers.any
import org.mockito.ArgumentMatchers.anyString
import org.mockito.Mockito.doThrow
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.dao.QueryTimeoutException
import org.springframework.transaction.CannotCreateTransactionException
import org.springframework.test.context.bean.override.mockito.MockitoSpyBean
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get
import java.time.Instant
import java.util.UUID

// #14 test 43, in its own spec because it stubs a repository: a refresh that
// could not be written is not a reason to fail a request the session already
// authorises.
class SessionTouchFailureSpec : WebDatabaseSpec() {

    @Autowired
    private lateinit var appConfig: AppConfig

    @MockitoSpyBean
    private lateinit var sessionRepository: SessionRepository

    // Mockito's any() returns null, which a Kotlin non-null parameter rejects
    // before the matcher is ever registered. The elvis keeps the call typed
    // while the matcher registers as the side effect Mockito expects.
    private fun anyInstant(): Instant = any(Instant::class.java) ?: Instant.EPOCH

    init {
        // aFailedTouchDoesNotFailTheRequest. Both families an outage arrives as
        // (#27): touch takes its connection when its transaction begins, so a
        // pool with none to lend throws CannotCreateTransactionException.
        listOf(
            QueryTimeoutException("touch failed"),
            CannotCreateTransactionException("touch failed"),
        ).forEach { failure ->
            "serves the request when the session refresh fails (${failure.javaClass.simpleName})" {
                val account = AuthFixtures.account(dataSource)
                val token = UUID.randomUUID().toString()
                AuthFixtures.session(
                    dataSource,
                    account.userId,
                    sha256Hex(token),
                    // Past half the TTL, so the filter will try to refresh.
                    expiresAt = Instant.now().plus(appConfig.sessionTtl.dividedBy(2)).minusSeconds(60),
                )
                doThrow(failure).`when`(sessionRepository).touch(anyString(), anyInstant())

                val result = mockMvc.perform(
                    get(AuthApi.BASE_PATH + AuthApi.PATH_GET_ME)
                        .cookie(Cookie(SessionCookieFactory.COOKIE_NAME, token))
                ).andReturn()

                // The session is valid until its current expiry regardless.
                result.response.status shouldBe 200
                // And no refreshed cookie is sent, because nothing was extended.
                result.response.getHeaders("Set-Cookie").firstOrNull().shouldBeNull()
            }
        }
    }
}
