package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.jsonFields
import dev.kerti.afloat.testsupport.sha256Hex
import io.kotest.matchers.collections.shouldNotContain
import io.kotest.matchers.ints.shouldBeGreaterThanOrEqual
import io.kotest.matchers.ints.shouldBeLessThanOrEqual
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldNotContain
import jakarta.servlet.http.Cookie
import org.springframework.test.web.servlet.MvcResult
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get
import java.math.BigDecimal
import java.time.Instant
import java.time.LocalTime
import java.util.UUID

// #14 tests 46-55. The first real money field on the wire, so this is where
// #13's Jackson configuration stops being theoretical.
class MeSpec : WebDatabaseSpec() {

    private val mePath = AuthApi.BASE_PATH + AuthApi.PATH_GET_ME

    private fun seedSessionFor(userId: UUID): String {
        val token = UUID.randomUUID().toString()
        AuthFixtures.session(
            dataSource, userId, sha256Hex(token),
            expiresAt = Instant.now().plusSeconds(3600),
        )
        return token
    }

    private fun me(token: String?): MvcResult =
        mockMvc.perform(
            get(mePath).apply { token?.let { cookie(Cookie(SessionCookieFactory.COOKIE_NAME, it)) } }
        ).andReturn()

    // Flat JSON, read without a mapper so the assertions are about the bytes on
    // the wire rather than about how a mapper chooses to read them back. The
    // reader lives in testsupport because SystemBodySpec asserts the same way.
    private fun body(result: MvcResult): Map<String, String> =
        result.response.contentAsString.jsonFields()

    init {
        // meReturnsUserAndHouseholdFieldsForAValidSession
        // meBodyHasNoAdditionalProperties
        "returns every required Me field and nothing more" {
            val account = AuthFixtures.account(dataSource)
            val token = seedSessionFor(account.userId)

            val result = me(token)

            result.response.status shouldBe 200
            val fields = body(result)
            // The contract's required list, exactly. expected_monthly_income is
            // absent rather than null: application.yaml pins
            // default-property-inclusion=non_null so Go and Kotlin omit it the
            // same way.
            fields.keys shouldBe setOf(
                "id", "household_id", "email", "display_name", "locale", "time_zone",
                "household_display_name", "reporting_currency", "period_start_day",
                "day_starts_at", "allowance_mode",
            )
            fields["id"] shouldBe "\"${account.userId}\""
            fields["household_id"] shouldBe "\"${account.householdId}\""
            fields["email"] shouldBe "\"user@example.com\""
            fields["display_name"] shouldBe "\"Rad\""
            fields["time_zone"] shouldBe "\"Asia/Jakarta\""
            fields["household_display_name"] shouldBe "\"Rumah Tangga\""
            fields["reporting_currency"] shouldBe "\"IDR\""
            fields["allowance_mode"] shouldBe "\"adaptive\""
        }

        // meReturns401UnauthorizedWithoutASession
        "answers 401 UNAUTHORIZED without a session" {
            val result = me(null)

            result.response.status shouldBe 401
            // Spring Security's entry point writes its own body from a filter,
            // outside controller exception handling. That is the trap.
            result.response.contentAsString shouldBe """{"code":"UNAUTHORIZED"}"""
        }

        // meScopesTheHouseholdByTheAuthenticatedUsersHouseholdId
        "scopes the household by the authenticated user's household" {
            val mine = AuthFixtures.account(
                dataSource, email = "mine@example.com", householdDisplayName = "Rumah Saya"
            )
            AuthFixtures.account(
                dataSource, email = "theirs@example.com", householdDisplayName = "Rumah Mereka"
            )
            val token = seedSessionFor(mine.userId)

            val result = me(token)

            // Read from the session's own User, never from anything the request
            // supplied (non-negotiable 3).
            body(result)["household_display_name"] shouldBe "\"Rumah Saya\""
            body(result)["household_id"] shouldBe "\"${mine.householdId}\""
        }

        // meIgnoresASoftDeletedHousehold
        "refuses to serve a user whose household is soft-deleted" {
            val account = AuthFixtures.account(
                dataSource, householdDeletedAt = Instant.now().minusSeconds(60)
            )
            val token = seedSessionFor(account.userId)

            val result = me(token)

            // A User whose Household is gone cannot be served coherently, so
            // the soft-deleted row is invisible and the request is an internal
            // error rather than a half-populated body. Go's GetMe answers the
            // same way for the same reason (me.go).
            result.response.status shouldBe 500
            result.response.contentAsString shouldBe """{"code":"INTERNAL"}"""
        }

        // expectedMonthlyIncomeSerialisesAsAStringOrNull
        "serialises expected_monthly_income as a string, scale and all" {
            val account = AuthFixtures.account(
                dataSource, expectedMonthlyIncome = BigDecimal("25000000.0000")
            )
            val token = seedSessionFor(account.userId)

            val fields = body(me(token))

            // A JSON number or scientific notation here is a bug, not a
            // formatting quirk (non-negotiable 1).
            fields["expected_monthly_income"] shouldBe "\"25000000.0000\""
            fields["expected_monthly_income"]!! shouldNotContain "E"
        }

        "omits expected_monthly_income when the household has none" {
            val account = AuthFixtures.account(dataSource, expectedMonthlyIncome = null)
            val token = seedSessionFor(account.userId)

            body(me(token)).keys shouldNotContain "expected_monthly_income"
        }

        // localeSerialisesAsTheContractEnum
        "serialises locale as the contract enum" {
            val english = AuthFixtures.account(dataSource, email = "en@example.com", locale = "en-GB")
            val indonesian = AuthFixtures.account(dataSource, email = "id@example.com", locale = "id-ID")

            // Both locales are real from day one (PRD N12a).
            body(me(seedSessionFor(english.userId)))["locale"] shouldBe "\"en-GB\""
            body(me(seedSessionFor(indonesian.userId)))["locale"] shouldBe "\"id-ID\""
        }

        // dayStartsAtSerialisesAsHhColonMm
        "serialises day_starts_at as HH:MM" {
            val account = AuthFixtures.account(dataSource, dayStartsAt = LocalTime.of(4, 0))
            val token = seedSessionFor(account.userId)

            // Not a LocalTime rendered as 04:00:00, which the contract's
            // pattern rejects.
            body(me(token))["day_starts_at"] shouldBe "\"04:00\""
        }

        "serialises a day_starts_at with minutes as HH:MM too" {
            val account = AuthFixtures.account(dataSource, dayStartsAt = LocalTime.of(23, 30))
            val token = seedSessionFor(account.userId)

            body(me(token))["day_starts_at"] shouldBe "\"23:30\""
        }

        // periodStartDayIsAnIntegerBetweenOneAndTwentyEight
        "serialises period_start_day as an integer within the contract's range" {
            val account = AuthFixtures.account(dataSource, periodStartDay = 28)
            val token = seedSessionFor(account.userId)

            val raw = body(me(token))["period_start_day"]
            raw shouldBe "28"
            // An integer, not a quoted string: 28 is a day of month, not money.
            raw!!.toInt() shouldBeGreaterThanOrEqual 1
            raw.toInt() shouldBeLessThanOrEqual 28
        }

        // meBodyCarriesNoPasswordHashOrSessionField
        "carries no credential or session material" {
            val account = AuthFixtures.account(dataSource)
            val token = seedSessionFor(account.userId)

            val raw = me(token).response.contentAsString

            // An entity serialised directly is how a credential reaches the wire.
            raw shouldNotContain "argon2"
            raw shouldNotContain "password"
            raw shouldNotContain "session"
            raw shouldNotContain token
            raw shouldNotContain sha256Hex(token)
            raw shouldNotContain "deleted_at"
        }
    }
}
