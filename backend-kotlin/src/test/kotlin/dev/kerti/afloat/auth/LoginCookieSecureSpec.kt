package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.InsecureCookieDatabaseInitializer
import dev.kerti.afloat.testsupport.SpringDatabaseSpec
import dev.kerti.afloat.testsupport.TestDatabase
import dev.kerti.afloat.testsupport.cookieAttributes
import dev.kerti.afloat.testsupport.loginBody
import dev.kerti.afloat.testsupport.sessionCookieHeader
import io.kotest.matchers.collections.shouldContain
import io.kotest.matchers.collections.shouldNotContain
import io.kotest.matchers.nulls.shouldNotBeNull
import io.kotest.matchers.shouldBe
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.webmvc.test.autoconfigure.AutoConfigureMockMvc
import org.springframework.http.MediaType
import org.springframework.test.context.ContextConfiguration
import org.springframework.test.web.servlet.MockMvc
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post

// #14 test 13. Its own context: COOKIE_SECURE is read once, when
// SessionCookieFactory is constructed, so it cannot be varied within a spec.
@AutoConfigureMockMvc
@ContextConfiguration(initializers = [InsecureCookieDatabaseInitializer::class])
class LoginCookieSecureSpec : SpringDatabaseSpec() {

    @Autowired
    private lateinit var mockMvc: MockMvc

    init {
        beforeTest { TestDatabase.truncate(dataSource) }

        // cookieSecureFollowsConfiguration
        "drops Secure when COOKIE_SECURE is false, and nothing else" {
            AuthFixtures.account(dataSource)

            val header = mockMvc.perform(
                post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
                    .contentType(MediaType.APPLICATION_JSON)
                    .content(loginBody("user@example.com", AuthFixtures.PASSWORD))
            ).andReturn().sessionCookieHeader()

            header.shouldNotBeNull()
            val attributes = header.cookieAttributes()
            // The self-hoster on plain HTTP over a LAN. Everything that is not
            // about transport stays exactly as it was.
            attributes.keys shouldNotContain "secure"
            attributes.keys shouldContain "httponly"
            attributes["path"] shouldBe "/"
            attributes["samesite"] shouldBe "Lax"
            attributes.keys shouldNotContain "domain"
        }
    }
}
