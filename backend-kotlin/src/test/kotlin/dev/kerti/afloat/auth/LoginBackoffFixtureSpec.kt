package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.auth.data.LoginAttemptRepository
import dev.kerti.afloat.config.DurationParser
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.loginBody
import io.kotest.assertions.withClue
import io.kotest.matchers.collections.shouldNotBeEmpty
import io.kotest.matchers.shouldBe
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.http.MediaType
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import tools.jackson.databind.json.JsonMapper
import java.io.File
import java.net.InetAddress

// The login backoff both backends must implement alike
// (contract/testdata/login_backoff.json, issue #16). Go's
// backoff_fixture_test.go reads the same file.
class LoginBackoffFixtureSpec : WebDatabaseSpec() {

    @Autowired
    private lateinit var loginAttempts: LoginAttemptRepository

    private val fixture = JsonMapper().readTree(File(System.getProperty("afloat.contract.testdata"), "login_backoff.json"))

    init {
        // The curve is the SQL (LoginAttemptRepository.recordFailure), so the
        // query is what gets driven, with the fixture's parameters: end to end,
        // the second failure would be answered 429 by the first one's window.
        "doubles per failure and caps as the shared curve says" {
            val first = DurationParser.parse(fixture.get("parameters").get("first_backoff").asString())
            val max = DurationParser.parse(fixture.get("parameters").get("max_backoff").asString())
            val curve = fixture.get("curve").toList()
            curve.shouldNotBeEmpty()

            curve.forEach { step ->
                loginAttempts.recordFailure("email:curve@example.com", first.toSecondsDouble(), max.toSecondsDouble())
                val (failures, window) = JdbcClient.create(dataSource)
                    .sql(
                        """
                        SELECT failure_count, extract(epoch FROM backoff_until - updated_at)::float8
                        FROM login_attempts WHERE key = 'email:curve@example.com'
                        """
                    )
                    .query { rs, _ -> rs.getInt(1) to rs.getDouble(2) }
                    .single()
                withClue("after failure ${step.get("failures").asInt()}") {
                    failures shouldBe step.get("failures").asInt()
                    window shouldBe step.get("backoff_seconds").asDouble()
                }
            }
        }

        // The keys one failed login writes, through the real request path. The
        // address is presented as Tomcat presents it - Java's own spelling, which
        // expands IPv6 - and must come back in the fixture's canonical form.
        "writes the keys the shared fixture lists" {
            val cases = fixture.get("keys").toList()
            cases.shouldNotBeEmpty()

            cases.forEach { case ->
                JdbcClient.create(dataSource).sql("TRUNCATE login_attempts").update()
                val email = case.get("email").asString()
                val ip = case.get("ip").asString()
                val tomcatSpelling = InetAddress.getByName(ip).hostAddress

                mockMvc.perform(
                    post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN)
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(loginBody(email, "not the password"))
                        .with { it.remoteAddr = tomcatSpelling; it }
                ).andReturn().response.status shouldBe 401

                val keys = JdbcClient.create(dataSource).sql("SELECT key FROM login_attempts ORDER BY key")
                    .query(String::class.java).list()
                withClue("$email from $ip, presented as $tomcatSpelling") {
                    keys shouldBe case.get("keys").toList().map { it.asString() }.sorted()
                }
            }
        }
    }
}
