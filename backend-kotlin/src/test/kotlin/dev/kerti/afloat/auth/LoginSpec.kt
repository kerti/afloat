package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.config.AppConfig
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.cookieAttributes
import dev.kerti.afloat.testsupport.cookieValue
import dev.kerti.afloat.testsupport.loginBody
import dev.kerti.afloat.testsupport.sessionCookieHeader
import dev.kerti.afloat.testsupport.sha256Hex
import io.kotest.assertions.withClue
import io.kotest.matchers.collections.shouldContain
import io.kotest.matchers.collections.shouldNotContain
import io.kotest.matchers.longs.shouldBeLessThan
import io.kotest.matchers.nulls.shouldNotBeNull
import io.kotest.matchers.shouldBe
import io.kotest.matchers.shouldNotBe
import io.kotest.matchers.string.shouldContain
import io.kotest.matchers.string.shouldNotContain
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.http.HttpHeaders
import org.springframework.http.MediaType
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import java.time.Duration
import java.time.Instant

// #14 tests 11-27: POST /api/auth/local/login, success and failure. The failure
// half is one code, one body and equal cost - BOOTSTRAP §5.1 wants all three,
// because any one missing re-opens account enumeration.
class LoginSpec : WebDatabaseSpec() {

    @Autowired
    private lateinit var appConfig: AppConfig

    private val loginPath = AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN

    // Each attempt comes from its own address by default. Three failures from
    // one IP would trip the backoff mid-test and answer 429 where the spec is
    // asking about credentials - which is correct behaviour, and covered in
    // LoginBackoffSpec rather than here.
    private var nextAddress = 1

    private fun login(email: String, password: String, from: String = "198.51.100.${nextAddress++}") =
        mockMvc.perform(
            post(loginPath).contentType(MediaType.APPLICATION_JSON)
                .content(loginBody(email, password))
                .with { it.remoteAddr = from; it }
        ).andReturn()

    init {
        // loginWithValidCredentialsReturns204WithNoBody
        "returns 204 and no body for valid credentials" {
            AuthFixtures.account(dataSource)

            val result = login("user@example.com", AuthFixtures.PASSWORD)

            result.response.status shouldBe 204
            result.response.contentAsString shouldBe ""
        }

        // loginSetsSessionCookieWithMandatedAttributes
        "sets the session cookie with the mandated attributes" {
            AuthFixtures.account(dataSource)

            val header = login("user@example.com", AuthFixtures.PASSWORD).sessionCookieHeader()

            header.shouldNotBeNull()
            val attributes = header.cookieAttributes()
            attributes["path"] shouldBe "/"
            attributes.keys shouldContain "httponly"
            attributes.keys shouldContain "secure"
            attributes["samesite"] shouldBe "Lax"
            // Host-only, deliberately: a cookie scoped to a shared parent domain
            // leaks a demo session into a preview deployment (BOOTSTRAP §5).
            attributes.keys shouldNotContain "domain"
            // The whole SESSION_TTL, not one second short of it. Max-Age is the
            // attribute a browser prefers over Expires, and Go emits the same
            // number (TestSessionCookieMaxAgeIsTheFullTTL); the rounding rules
            // that make the two agree are pinned in SessionCookieFactorySpec.
            attributes["max-age"] shouldBe appConfig.sessionTtl.toSeconds().toString()
        }

        // sessionRowStoresTheSha256OfTheTokenNeverTheToken
        "stores the SHA-256 of the token and never the token" {
            AuthFixtures.account(dataSource)

            val token = login("user@example.com", AuthFixtures.PASSWORD).sessionCookieHeader()!!.cookieValue()

            val row = JdbcClient.create(dataSource)
                .sql("SELECT id, user_id::text AS user_id, user_agent FROM sessions")
                .query().singleRow()
            row["id"] shouldBe sha256Hex(token)
            // The whole row, not just the id: a token copied into user_agent or
            // anywhere else is the same leak.
            row.values.joinToString("|") { it?.toString().orEmpty() } shouldNotContain token
        }

        // sessionExpiresAtIsNowPlusSessionTtl
        "expires the session one SESSION_TTL from now" {
            AuthFixtures.account(dataSource)
            val before = Instant.now()

            login("user@example.com", AuthFixtures.PASSWORD)

            val expiresAt = JdbcClient.create(dataSource)
                .sql("SELECT expires_at FROM sessions").query(Instant::class.java).single()
            val drift = Duration.between(before.plus(appConfig.sessionTtl), expiresAt).abs()
            withClue("expires_at drifted ${drift.toSeconds()}s from now + ${appConfig.sessionTtl}") {
                drift.toSeconds() shouldBeLessThan 60
            }
        }

        // successfulLoginClearsBackoffRowsForBothKeys
        "clears the backoff rows for both keys on success" {
            AuthFixtures.account(dataSource)
            // Expired windows: present, counting, but not blocking this attempt.
            AuthFixtures.loginAttempt(dataSource, "email:user@example.com", 3, Instant.now().minusSeconds(60))
            AuthFixtures.loginAttempt(dataSource, "ip:203.0.113.9", 3, Instant.now().minusSeconds(60))

            login("user@example.com", AuthFixtures.PASSWORD, from = "203.0.113.9").response.status shouldBe 204

            // A bad evening must not throttle the household for the rest of it.
            JdbcClient.create(dataSource).sql("SELECT count(*) FROM login_attempts")
                .query(Long::class.java).single() shouldBe 0L
        }

        // emailIsMatchedCaseInsensitivelyAndTrimmed (case half)
        "matches the email case-insensitively" {
            AuthFixtures.account(dataSource, email = "user@example.com")

            // The unique index is lower(email), so the lookup expression has to
            // agree with it or a signup and a login disagree about identity.
            login("User@Example.COM", AuthFixtures.PASSWORD).response.status shouldBe 204
        }

        // The case above only ever proved the INPUT is normalised, because the
        // fixture stored a lower-case address. The column is plain `text` and
        // only the INDEX is lower(email), so the stored side can carry case
        // too — and a repository comparing `email = :email` misses it while
        // Go's `lower(email) = lower($1)` finds it.
        "matches an address stored with capitals" {
            AuthFixtures.account(dataSource, email = "Mixed.Case@Example.COM")

            withClue("normalised input against a stored capital") {
                login("mixed.case@example.com", AuthFixtures.PASSWORD).response.status shouldBe 204
            }
            withClue("capitals on both sides") {
                login("Mixed.Case@Example.COM", AuthFixtures.PASSWORD).response.status shouldBe 204
            }
        }

        // emailIsMatchedCaseInsensitivelyAndTrimmed (trimming half). #18 ruled
        // "trim, then validate": @Email judges the trimmed address
        // (TrimmedEmailValidator), and normalizeEmail trims again for the lookup.
        "logs in with a padded address" {
            AuthFixtures.account(dataSource, email = "user@example.com")

            login("  User@Example.COM  ", AuthFixtures.PASSWORD).response.status shouldBe 204
            // Control-character padding too, which Go's net/mail-backed decode
            // once refused after the format check had accepted it.
            mockMvc.perform(
                post(loginPath).contentType(MediaType.APPLICATION_JSON)
                    .content("""{"email":"\n  User@Example.COM\t","password":"${AuthFixtures.PASSWORD}"}""")
                    .with { it.remoteAddr = "198.51.100.${nextAddress++}"; it }
            ).andReturn().response.status shouldBe 204
        }

        // loginIgnoresASoftDeletedUser
        "refuses a soft-deleted user" {
            AuthFixtures.account(dataSource, userDeletedAt = Instant.now().minusSeconds(3600))

            val result = login("user@example.com", AuthFixtures.PASSWORD)

            result.response.status shouldBe 401
            result.response.contentAsString shouldContain "INVALID_CREDENTIALS"
            JdbcClient.create(dataSource).sql("SELECT count(*) FROM sessions")
                .query(Long::class.java).single() shouldBe 0L
        }

        // unknownEmailReturns401InvalidCredentials
        // userWithNoCredentialReturns401InvalidCredentials
        // wrongPasswordReturns401InvalidCredentials
        // allThreeFailureModesReturnTheIdenticalBody
        "answers every failure mode with one identical 401" {
            AuthFixtures.account(dataSource, email = "has-password@example.com")
            // The dormant User: invited, owns data, never set a password. A
            // legitimate state, and not one the response may reveal.
            AuthFixtures.account(dataSource, email = "dormant@example.com", withCredential = false)

            val unknown = login("nobody@example.com", AuthFixtures.PASSWORD)
            val dormant = login("dormant@example.com", AuthFixtures.PASSWORD)
            val wrongPassword = login("has-password@example.com", "not the password")

            listOf(unknown, dormant, wrongPassword).forEach {
                it.response.status shouldBe 401
            }
            val bodies = listOf(unknown, dormant, wrongPassword).map { it.response.contentAsString }
            bodies.distinct() shouldBe listOf("""{"code":"INVALID_CREDENTIALS"}""")
        }

        // passwordComparisonIsConstantTime
        "compares the hash with a constant-time primitive" {
            // Argon2PasswordEncoder compares through MessageDigest.isEqual;
            // this pins that PasswordService has not been reimplemented around
            // String.equals, which leaks how much of the hash matched.
            val source = java.nio.file.Path.of(
                "src/main/kotlin/dev/kerti/afloat/auth/PasswordService.kt"
            ).toFile().readText()
            source shouldContain "Argon2PasswordEncoder"
            source shouldNotContain "phc =="
            source shouldNotContain "hash =="
        }

        // malformedJsonBodyReturns400InvalidJsonBody
        "rejects a malformed JSON body with INVALID_JSON_BODY" {
            val result = mockMvc.perform(
                post(loginPath).contentType(MediaType.APPLICATION_JSON).content("{\"email\": ")
            ).andReturn()

            result.response.status shouldBe 400
            result.response.contentAsString shouldBe """{"code":"INVALID_JSON_BODY"}"""
        }

        // missingRequiredFieldReturns400ValidationWithFieldAndRule (#18)
        "reports a missing required field as VALIDATION required" {
            val result = mockMvc.perform(
                post(loginPath).contentType(MediaType.APPLICATION_JSON).content("""{"email":"user@example.com"}""")
            ).andReturn()

            result.response.status shouldBe 400
            result.response.contentAsString shouldBe """{"code":"VALIDATION","args":{"field":"password","rule":"required"}}"""
        }

        // The rest of the table Go's login_validation_test.go drives, body for
        // body. An absent field competes with the present ones for the one
        // field reported (AbsentFieldAdvice); a body that is not the declared
        // shape is INVALID_JSON_BODY whatever else is wrong with it.
        "answers each body exactly as Go does" {
            AuthFixtures.account(dataSource)
            val validation = { field: String, rule: String ->
                """{"code":"VALIDATION","args":{"field":"$field","rule":"$rule"}}"""
            }
            val invalidJson = """{"code":"INVALID_JSON_BODY"}"""
            listOf(
                "" to invalidJson,
                " \n\t" to invalidJson,
                // Jackson reads null, and Spring then refuses a required body
                // that is null.
                "null" to invalidJson,
                """{}""" to validation("email", "required"),
                """{"password":"x"}""" to validation("email", "required"),
                // kin-openapi and Jackson would both reach password first; the
                // sort says email.
                """{"password":""}""" to validation("email", "required"),
                """{"email":"not-an-email"}""" to validation("email", "email"),
                // Hibernate's @Email alone calls "" valid.
                """{"email":"","password":"x"}""" to validation("email", "email"),
                """{"email":"   ","password":"x"}""" to validation("email", "email"),
                """{"email":"  user@example.com  "}""" to validation("password", "required"),
                """{"email":5,"password":"x"}""" to invalidJson,
                """{"email":"user@example.com","password":true}""" to invalidJson,
                """{"email":null,"password":"x"}""" to invalidJson,
                """{"password":null}""" to invalidJson,
                """{"password":"","admin":true}""" to invalidJson,
                """{"email":"not-an-email","password":"x","zzz":1}""" to invalidJson,
                """["user@example.com"]""" to invalidJson,
                """{"email":"user@example.com","password":"x"} x""" to invalidJson,
                """{"password":"x"} x""" to invalidJson,
                """{"email":"user@example.com","password":"x"}{}""" to invalidJson,
            ).forEach { (body, expected) ->
                withClue(body) {
                    val result = mockMvc.perform(
                        post(loginPath).contentType(MediaType.APPLICATION_JSON).content(body)
                    ).andReturn()

                    result.response.status shouldBe 400
                    result.response.contentAsString shouldBe expected
                }
            }
            // None of them got as far as the handler.
            JdbcClient.create(dataSource).sql("SELECT count(*) FROM login_attempts")
                .query(Long::class.java).single() shouldBe 0L
        }

        // Go's rows that no String body can carry: bytes that are not UTF-8 JSON
        // text, which Jackson alone would read (JsonTextAdvice), and a JSON body
        // not declared as JSON (a null type sends no Content-Type at all).
        "answers a body that is not UTF-8 JSON exactly as Go does" {
            AuthFixtures.account(dataSource)
            fun bytes(vararg parts: Any): ByteArray = parts.fold(ByteArray(0)) { acc, part ->
                acc + when (part) {
                    is String -> part.toByteArray()
                    is Int -> byteArrayOf(part.toByte())
                    else -> error("unexpected part $part")
                }
            }
            val valid = loginBody("user@example.com", AuthFixtures.PASSWORD)
            val notUtf8 = bytes("""{"email":"user@example.com","password":"x""", 0xff, """"}""")
            val json = MediaType.APPLICATION_JSON_VALUE
            listOf(
                Triple("not UTF-8", json, notUtf8),
                Triple("overlong UTF-8", json, bytes("""{"email":"user@example.com","password":"x""", 0xc0, 0xa0, """"}""")),
                Triple("overlong UTF-8 beside an absent field", json, bytes("""{"password":"x""", 0xc0, 0xa0, """"}""")),
                Triple("byte order mark", json, bytes(0xef, 0xbb, 0xbf, valid)),
                Triple("UTF-16", json, valid.toByteArray(Charsets.UTF_16LE)),
                // application/json has no charset parameter (RFC 8259 §11).
                Triple("not UTF-8, declared as Latin-1", "application/json; charset=ISO-8859-1", notUtf8),
                Triple("not declared as JSON", MediaType.TEXT_PLAIN_VALUE, valid.toByteArray()),
                Triple("not declared at all", null, valid.toByteArray()),
                // Only ASCII letters fold: U+0130 lowercases to "i" in Unicode.
                Triple("declared with a non-ASCII letter", "appl\u0130cation/json", valid.toByteArray()),
                // Spring's JSON converter reads any +json type; the contract
                // declares application/json alone.
                Triple("declared as another JSON type", "application/vnd.api+json", valid.toByteArray()),
            ).forEach { (name, type, body) ->
                withClue(name) {
                    val request = post(loginPath).content(body)
                    type?.let { request.header(HttpHeaders.CONTENT_TYPE, it) }
                    val result = mockMvc.perform(request).andReturn()

                    result.response.status shouldBe 400
                    result.response.contentAsString shouldBe """{"code":"INVALID_JSON_BODY"}"""
                }
            }
            JdbcClient.create(dataSource).sql("SELECT count(*) FROM login_attempts")
                .query(Long::class.java).single() shouldBe 0L
        }

        // A media type is case-insensitive and may have whitespace around it
        // (RFC 9110 §8.3.1), and a body is UTF-8 whatever charset is claimed,
        // even one the JDK does not know. Every other parameter is ignored, even
        // one Spring could not parse (Utf8CharsetFilter). Go's
        // TestLoginReadsEverySpellingOfTheJSONMediaType sends the same headers.
        "reads every spelling of the JSON media type as Go does" {
            listOf(
                "Application/JSON",
                "APPLICATION/JSON; CHARSET=UTF-8",
                "application/json ; charset=utf-8",
                "\tapplication/json\t;charset=utf-8",
                "application/json; charset=utf-16",
                "application/json; charset=bogus",
                "application/json; foo=a b",
                "application/json; foo=",
                "application/json; foo=\"bar",
            ).forEach { contentType ->
                withClue(contentType) {
                    val result = mockMvc.perform(
                        post(loginPath).header(HttpHeaders.CONTENT_TYPE, contentType)
                            .content("""{"email":"not-an-email","password":"a valid password"}""")
                    ).andReturn()

                    result.response.status shouldBe 400
                    result.response.contentAsString shouldBe """{"code":"VALIDATION","args":{"field":"email","rule":"email"}}"""
                }
            }
        }

        // The envelope #14 test 26 is actually about: a field that is present
        // and invalid does reach Bean Validation, and carries {field, rule} for
        // the first failing field only.
        "reports the first failing field with field and rule" {
            val result = mockMvc.perform(
                post(loginPath).contentType(MediaType.APPLICATION_JSON)
                    .content("""{"email":"user@example.com","password":""}""")
            ).andReturn()

            result.response.status shouldBe 400
            result.response.contentAsString shouldContain """"code":"VALIDATION""""
            result.response.contentAsString shouldContain """"field":"password""""
            // @Size(min=1,max=4096) is one constraint covering both bounds,
            // where Go spells them as separate `min=` and `max=` tags and
            // reports whichever failed (httperr_test.go). Reporting "max" for
            // an empty password would send the frontend to the "too long" key.
            result.response.contentAsString shouldContain """"rule":"min""""
        }

        // Every request schema in the contract declares additionalProperties:
        // false, and spring.jackson.deserialization.fail-on-unknown-properties
        // makes Jackson honour it (#13 §3.6). Go's spec-validating middleware
        // answers the same (#18).
        "rejects a body carrying a field the contract does not declare" {
            AuthFixtures.account(dataSource)

            val result = mockMvc.perform(
                post(loginPath).contentType(MediaType.APPLICATION_JSON)
                    .content(
                        """{"email":"user@example.com","password":"${AuthFixtures.PASSWORD}","admin":true}"""
                    )
            ).andReturn()

            result.response.status shouldBe 400
            result.response.contentAsString shouldBe """{"code":"INVALID_JSON_BODY"}"""
            // The point of refusing it: the field was not quietly dropped and
            // the login did not succeed anyway.
            JdbcClient.create(dataSource).sql("SELECT count(*) FROM sessions")
                .query(Long::class.java).single() shouldBe 0L
        }

        // Two fields fail at once, and the envelope reports one. WHICH one must
        // not depend on the run: Bean Validation collects violations in an
        // unspecified order, so this is the assertion that the handler sorts
        // rather than taking whatever the JVM handed it (#13 §3.5).
        "reports the same field every time when two of them fail" {
            val bodies = (1..8).map {
                mockMvc.perform(
                    post(loginPath).contentType(MediaType.APPLICATION_JSON)
                        .content("""{"email":"not-an-email","password":""}""")
                ).andReturn().response.contentAsString
            }

            bodies.distinct().size shouldBe 1
            bodies.first() shouldContain """"field":"email""""
        }

        // emailLongerThan320OrPasswordLongerThan4096IsRejected
        "rejects an over-long email or password" {
            val longEmail = "a".repeat(310) + "@example.com" // 322 characters
            val overLongEmail = login(longEmail, AuthFixtures.PASSWORD)
            val overLongPassword = login("user@example.com", "a".repeat(4097))
            // Length is counted in code points, as JSON Schema and Go count it
            // (TestLoginCountsPasswordLengthInCodePoints): an emoji is one
            // character, not two UTF-16 units.
            val emoji = "\uD83D\uDE00"
            val longestEmojiPassword = login("user@example.com", emoji.repeat(4096))
            val overLongEmojiPassword = login("user@example.com", emoji.repeat(4097))
            // Read as UTF-8 whatever the header claims (Utf8CharsetFilter;
            // TestLoginReadsABodyDeclaredLatin1AsUTF8): 4096 "é" are 4096
            // characters, not the 8192 a Latin-1 reading makes of their bytes.
            val latin1Password = mockMvc.perform(
                post(loginPath).contentType(MediaType(MediaType.APPLICATION_JSON, Charsets.ISO_8859_1))
                    .content(loginBody("latin1@example.com", "é".repeat(4096)).toByteArray())
                    .with { it.remoteAddr = "198.51.100.${nextAddress++}"; it }
            ).andReturn()

            withClue("email of ${longEmail.length} characters") {
                overLongEmail.response.status shouldBe 400
                overLongEmail.response.contentAsString shouldContain "\"field\":\"email\""
            }
            withClue("password of 4097 characters") {
                overLongPassword.response.status shouldBe 400
                overLongPassword.response.contentAsString shouldContain "\"field\":\"password\""
                // The other bound of the same @Size: the ceiling still reports
                // "max", so recovering the failing bound did not invert it.
                overLongPassword.response.contentAsString shouldContain "\"rule\":\"max\""
            }
            withClue("password of 4096 emoji") {
                longestEmojiPassword.response.status shouldBe 401
            }
            withClue("password of 4097 emoji") {
                overLongEmojiPassword.response.contentAsString shouldBe
                    """{"code":"VALIDATION","args":{"field":"password","rule":"max"}}"""
            }
            withClue("password of 4096 characters declared as Latin-1") {
                latin1Password.response.status shouldBe 401
            }
        }
    }
}
