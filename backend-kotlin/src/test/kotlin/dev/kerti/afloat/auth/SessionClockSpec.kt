package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.config.AppConfig
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.loginBody
import io.kotest.assertions.withClue
import io.kotest.matchers.doubles.shouldBeLessThan
import io.kotest.matchers.shouldBe
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.http.MediaType
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.context.bean.override.convention.TestBean
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import java.time.Clock
import java.time.Duration

// Any instant the database also evaluates comes from the database (#25,
// BOOTSTRAP.md §5.1). findLive checks the absolute cap as
// `created_at > now() - max_lifetime`, so a created_at written from an app
// clock two hours ahead would stretch every session's cap by two hours. Go's
// CreateSession leaves created_at and last_seen_at to the schema default; this
// pins that Kotlin does too, under an app clock that is plainly wrong.
class SessionClockSpec : WebDatabaseSpec() {

    @TestBean(name = "clock", methodName = "twoHoursAhead")
    private lateinit var clock: Clock

    @Autowired
    private lateinit var appConfig: AppConfig

    companion object {
        @JvmStatic
        fun twoHoursAhead(): Clock = Clock.offset(Clock.systemUTC(), Duration.ofHours(2))
    }

    private fun login() = mockMvc.perform(
        post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
            .contentType(MediaType.APPLICATION_JSON)
            .content(loginBody("user@example.com", AuthFixtures.PASSWORD))
    ).andReturn().response

    init {
        "writes a session's created_at and last_seen_at from the database's clock" {
            AuthFixtures.account(dataSource)

            login().status shouldBe 204

            val (createdSkew, seenSkew) = JdbcClient.create(dataSource)
                .sql(
                    """
                    SELECT abs(extract(epoch FROM now() - created_at))::float8,
                           abs(extract(epoch FROM now() - last_seen_at))::float8
                    FROM sessions
                    """
                )
                .query { rs, _ -> rs.getDouble(1) to rs.getDouble(2) }
                .single()
            withClue("seconds between the database's now() and created_at / last_seen_at") {
                createdSkew shouldBeLessThan 60.0
                seenSkew shouldBeLessThan 60.0
            }
        }

        // The consequence the rule exists for. Aged in SQL to one minute past
        // the cap, measured by the database: refused, however far ahead the app
        // clock runs. Written from that clock, the row would still have two
        // hours to go.
        "bounds the session by SESSION_MAX_LIFETIME from the row's real creation" {
            AuthFixtures.account(dataSource)
            val cookie = login().getCookie(SessionCookieFactory.COOKIE_NAME)!!

            JdbcClient.create(dataSource)
                .sql("UPDATE sessions SET created_at = created_at - make_interval(secs => :secs)")
                .param("secs", appConfig.sessionMaxLifetime.plusMinutes(1).seconds)
                .update()

            mockMvc.perform(get(AuthApi.BASE_PATH + AuthApi.PATH_GET_ME).cookie(cookie))
                .andReturn().response.status shouldBe 401
        }
    }
}
