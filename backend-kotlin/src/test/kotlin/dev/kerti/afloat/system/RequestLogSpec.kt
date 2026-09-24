package dev.kerti.afloat.system

import ch.qos.logback.classic.Level
import ch.qos.logback.classic.Logger
import ch.qos.logback.classic.spi.ILoggingEvent
import ch.qos.logback.core.read.ListAppender
import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.auth.SessionCookieFactory
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.loginBody
import io.kotest.assertions.withClue
import io.kotest.matchers.collections.shouldHaveSize
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain
import io.kotest.matchers.string.shouldNotContain
import jakarta.servlet.http.Cookie
import org.slf4j.LoggerFactory
import org.springframework.http.MediaType
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post

// #13 test 29 and PRD N6. A log line carrying an Expense description is
// telemetry that happens to be written to disk, and the fact that nobody meant
// it that way is not a defense.
class RequestLogSpec : WebDatabaseSpec() {

    private val appender = ListAppender<ILoggingEvent>()
    private val logger = LoggerFactory.getLogger(RequestLogFilter::class.java) as Logger

    private fun lines(): List<String> = appender.list.map { it.formattedMessage }

    init {
        beforeTest {
            appender.list.clear()
            appender.start()
            logger.addAppender(appender)
            logger.level = Level.INFO
        }
        afterTest { logger.detachAppender(appender) }

        // requestLogDoesNotContainBodyOrCookieValues
        "records method, path, status, duration and request id - and nothing else" {
            AuthFixtures.account(dataSource)
            val secret = AuthFixtures.PASSWORD

            mockMvc.perform(
                post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
                    .contentType(MediaType.APPLICATION_JSON)
                    .content(loginBody("user@example.com", secret))
                    .cookie(Cookie(SessionCookieFactory.COOKIE_NAME, "a-token-that-must-not-be-logged"))
                    .header("User-Agent", "afloat-pwa/1.0")
                    .with { it.remoteAddr = "198.51.100.61"; it },
            ).andReturn().response.status shouldBe 204

            lines() shouldHaveSize 1
            val line = lines().single()

            withClue(line) {
                line shouldContain "method=POST"
                line shouldContain "path=/api/auth/local/login"
                line shouldContain "status=204"
                line shouldContain "duration_ms="
                line shouldContain "request_id="

                // The body, the credential, the cookie the client presented,
                // and the address it came from. None of them belong on disk.
                line shouldNotContain secret
                line shouldNotContain "user@example.com"
                line shouldNotContain "a-token-that-must-not-be-logged"
                line shouldNotContain "198.51.100.61"
                line shouldNotContain "afloat-pwa"
            }
        }

        // The path, never the query string: requestURI stops at the '?', which
        // is half of how "nothing else" stays true once an endpoint takes
        // filters.
        "records the path without the query string" {
            val result = mockMvc.perform(
                post(AuthApi.BASE_PATH + AuthApi.PATH_LOGOUT + "?note=groceries&amount=50000"),
            ).andReturn()

            result.response.status shouldBe 204
            withClue(lines().single()) {
                lines().single() shouldContain "path=/api/auth/logout"
                lines().single() shouldNotContain "groceries"
                lines().single() shouldNotContain "50000"
            }
        }

        // A request refused by the cross-site guard is still a request. Go's
        // logger wraps everything below it, so this one is registered as a
        // servlet filter outside the security chain rather than as a link in it.
        "records a request the security chain refused" {
            val result = mockMvc.perform(
                post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN).header("Sec-Fetch-Site", "cross-site"),
            ).andReturn()

            result.response.status shouldBe 403
            lines().single() shouldContain "status=403"
        }

        // An inbound id is honored so a proxy's correlation survives, and
        // sanitized because in a self-hosted deployment nothing strips it: a
        // newline in that header would otherwise write a second, forged line.
        "honors a supplied request id but strips anything that could forge a line" {
            mockMvc.perform(
                post(AuthApi.BASE_PATH + AuthApi.PATH_LOGOUT)
                    .header(RequestLogFilter.REQUEST_ID_HEADER, "abc-123\nrequest method=GET path=/forged"),
            ).andReturn()

            val line = lines().single()
            withClue(line) {
                line shouldContain "request_id=abc-123requestmethodGETpathforged"
                line shouldNotContain "\n"
            }
        }

        "keeps only ASCII id alphabet from a supplied request id" {
            mockMvc.perform(
                post(AuthApi.BASE_PATH + AuthApi.PATH_LOGOUT)
                    .header(RequestLogFilter.REQUEST_ID_HEADER, "ab<c>d;f/g:iéÃ1"),
            ).andReturn()

            lines().single() shouldContain "request_id=abcdfgi1"
        }
    }
}
