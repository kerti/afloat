package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.auth.data.Session
import dev.kerti.afloat.auth.data.SessionRepository
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.loggedBy
import dev.kerti.afloat.testsupport.loginBody
import io.kotest.matchers.collections.shouldBeEmpty
import io.kotest.matchers.collections.shouldContain
import io.kotest.matchers.shouldBe
import org.mockito.ArgumentMatchers.any
import org.mockito.Mockito.doThrow
import org.springframework.dao.DataAccessResourceFailureException
import org.springframework.http.MediaType
import org.springframework.test.context.bean.override.mockito.MockitoSpyBean
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import org.springframework.transaction.CannotCreateTransactionException

// #23: a session that could not be written is a 500 from login's own catch,
// with either family an outage arrives as. Its own spec, not
// DatabaseOutageSpec: with SessionRepository spied, the real saveAndFlush
// fails "cannot execute INSERT in a read-only transaction" (the interface is
// @Transactional(readOnly = true)), so no login sharing the context succeeds.
class SessionIssueFailureSpec : WebDatabaseSpec() {

    @MockitoSpyBean
    private lateinit var sessionRepository: SessionRepository

    init {
        listOf(
            CannotCreateTransactionException("database down"),
            DataAccessResourceFailureException("database down"),
        ).forEach { outage ->
            "answers 500 when the session cannot be issued (${outage.javaClass.simpleName})" {
                val account = AuthFixtures.account(dataSource)
                doThrow(outage).`when`(sessionRepository).saveAndFlush(any(Session::class.java))

                val (result, logged) = loggedBy(AuthService::class.java) {
                    mockMvc.perform(
                        post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
                            .contentType(MediaType.APPLICATION_JSON)
                            .content(loginBody(account.email, AuthFixtures.PASSWORD))
                    ).andReturn()
                }

                result.response.status shouldBe 500
                result.response.contentAsString shouldBe """{"code":"INTERNAL"}"""
                result.response.getHeaders("Set-Cookie").shouldBeEmpty()
                logged shouldContain "login: issue session"
            }
        }
    }
}
