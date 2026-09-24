package dev.kerti.afloat.auth

import com.zaxxer.hikari.HikariDataSource
import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.auth.data.CredentialRepository
import dev.kerti.afloat.auth.data.UserRepository
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.loginBody
import dev.kerti.afloat.testsupport.sha256Hex
import io.kotest.matchers.collections.shouldBeEmpty
import io.kotest.matchers.shouldBe
import jakarta.servlet.http.Cookie
import org.mockito.ArgumentMatchers.any
import org.mockito.ArgumentMatchers.anyString
import org.mockito.Mockito.doThrow
import org.springframework.beans.factory.config.BeanPostProcessor
import org.springframework.boot.test.context.TestConfiguration
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Import
import org.springframework.dao.DataAccessResourceFailureException
import org.springframework.http.MediaType
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.context.bean.override.mockito.MockitoSpyBean
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import org.springframework.transaction.CannotCreateTransactionException
import java.sql.Connection
import java.util.UUID

// #23 and #27: a database failure is not a statement about this account's
// credentials or this user's session. Login answers 500 and burns no backoff;
// session resolution leaves the cookie alone and the request unauthenticated.
//
// Two ways in. A stubbed repository reaches the exact lookup each issue names
// (Go's failingQuerier does the same), with both exception families an outage
// can arrive as. An exhausted pool is the outage itself, with no stub deciding
// what it throws - which is how CannotCreateTransactionException was found.
@Import(DatabaseOutageSpec.ShortPoolTimeout::class)
class DatabaseOutageSpec : WebDatabaseSpec() {

    // Hikari's 30s default would make every exhausted-pool test a half-minute
    // wait. AppConfiguration builds the DataSource by hand, so
    // spring.datasource.hikari.* never reaches it, and setting the timeout on
    // a running pool only lands at Hikari's next 30s housekeeping tick. So it
    // is set here, before the pool starts. The spy beans already give this
    // spec a context of its own, so no other spec's pool is bent.
    @TestConfiguration
    class ShortPoolTimeout {
        companion object {
            @Bean
            @JvmStatic
            fun shortPoolTimeout() = object : BeanPostProcessor {
                override fun postProcessAfterInitialization(bean: Any, beanName: String): Any {
                    if (bean is HikariDataSource) bean.connectionTimeout = 250
                    return bean
                }
            }
        }
    }

    @MockitoSpyBean
    private lateinit var userRepository: UserRepository

    @MockitoSpyBean
    private lateinit var credentialRepository: CredentialRepository

    private val outages = listOf(
        CannotCreateTransactionException("database down"),
        DataAccessResourceFailureException("database down"),
    )

    // Mockito's any() returns null, which a Kotlin non-null parameter rejects
    // (same trick as SessionTouchFailureSpec.anyInstant).
    private fun anyUuid(): UUID = any(UUID::class.java) ?: UUID(0, 0)

    private fun login(email: String) = mockMvc.perform(
        post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
            .contentType(MediaType.APPLICATION_JSON)
            .content(loginBody(email, AuthFixtures.PASSWORD))
    ).andReturn()

    private fun me(token: String) = mockMvc.perform(
        get(AuthApi.BASE_PATH + AuthApi.PATH_GET_ME)
            .cookie(Cookie(SessionCookieFactory.COOKIE_NAME, token))
    ).andReturn()

    private fun loginAttemptKeys(): List<String?> =
        JdbcClient.create(dataSource).sql("SELECT key FROM login_attempts")
            .query(String::class.java).list()

    private fun liveSession(): String {
        val account = AuthFixtures.account(dataSource)
        val token = UUID.randomUUID().toString()
        AuthFixtures.session(dataSource, account.userId, sha256Hex(token))
        return token
    }

    // Holds every connection the pool will lend for the length of block, so
    // anything else that asks waits out the connection timeout and fails.
    private fun <T> withPoolExhausted(block: () -> T): T {
        val pool = dataSource.unwrap(HikariDataSource::class.java)
        val held = mutableListOf<Connection>()
        try {
            repeat(pool.maximumPoolSize) { held += pool.connection }
            return block()
        } finally {
            held.forEach { it.close() }
        }
    }

    init {
        outages.forEach { outage ->
            val kind = outage.javaClass.simpleName

            "answers 500 and records no failure when the user lookup fails ($kind)" {
                val account = AuthFixtures.account(dataSource)
                doThrow(outage).`when`(userRepository).findByEmail(anyString())

                val result = login(account.email)

                result.response.status shouldBe 500
                result.response.contentAsString shouldBe """{"code":"INTERNAL"}"""
                loginAttemptKeys().shouldBeEmpty()
            }

            "answers 500 and records no failure when the credential lookup fails ($kind)" {
                val account = AuthFixtures.account(dataSource)
                doThrow(outage).`when`(credentialRepository).findByUserId(account.userId)

                val result = login(account.email)

                result.response.status shouldBe 500
                result.response.contentAsString shouldBe """{"code":"INTERNAL"}"""
                loginAttemptKeys().shouldBeEmpty()
            }

            "leaves the cookie alone when the session's user lookup fails ($kind)" {
                val token = liveSession()
                doThrow(outage).`when`(userRepository).findById(anyUuid())

                val result = me(token)

                result.response.status shouldBe 401
                result.response.getHeaders("Set-Cookie").shouldBeEmpty()
            }
        }

        "answers 500 and records no failure when the pool is exhausted during login" {
            val account = AuthFixtures.account(dataSource)

            val result = withPoolExhausted { login(account.email) }

            result.response.status shouldBe 500
            result.response.contentAsString shouldBe """{"code":"INTERNAL"}"""
            loginAttemptKeys().shouldBeEmpty()
        }

        "leaves the cookie alone when the pool is exhausted during session resolution" {
            val token = liveSession()

            val result = withPoolExhausted { me(token) }

            result.response.status shouldBe 401
            result.response.contentAsString shouldBe """{"code":"UNAUTHORIZED"}"""
            result.response.getHeaders("Set-Cookie").shouldBeEmpty()
        }

        // The pool recovers and so does the session: the cookie that was left
        // alone still works, which is the whole point of leaving it alone.
        "serves the same session once the pool is back" {
            val token = liveSession()
            withPoolExhausted { me(token) }

            me(token).response.status shouldBe 200
        }
    }
}
