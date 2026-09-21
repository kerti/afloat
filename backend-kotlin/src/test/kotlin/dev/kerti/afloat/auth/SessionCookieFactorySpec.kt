package dev.kerti.afloat.auth

import dev.kerti.afloat.config.AppConfig
import dev.kerti.afloat.testsupport.TestConfigDefaults
import dev.kerti.afloat.testsupport.cookieAttributes
import io.kotest.assertions.withClue
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset

// Max-Age is the attribute a browser PREFERS over Expires, so on a client with
// a skewed clock it is the one that decides how long the session lives. Go
// computes it as `max(int(math.Ceil(expires.Sub(h.now()).Seconds())), 1)`
// (backend/internal/auth/session.go) and until these tests existed nothing on
// either side pinned the value, so the two were free to round differently —
// which they did.
class SessionCookieFactorySpec : StringSpec({

    val now = Instant.parse("2026-09-21T10:00:00Z")

    fun factoryAt(instant: Instant, cookieSecure: Boolean = true): SessionCookieFactory {
        val config = AppConfig.parse(
            TestConfigDefaults.testConfigBase("COOKIE_SECURE" to cookieSecure.toString())
        )
        return SessionCookieFactory(config, Clock.fixed(instant, ZoneOffset.UTC))
    }

    fun maxAgeOf(expires: Instant, at: Instant = now): String? =
        factoryAt(at).set("a-token", expires).cookieAttributes()["max-age"]

    "carries the whole remaining window as Max-Age" {
        maxAgeOf(now.plusSeconds(2_592_000)) shouldBe "2592000"
    }

    // The failure this replaced: truncation. The caller reads the clock to
    // compute `expires` and the factory reads it again microseconds later, so
    // a floor turned every single login's Max-Age into SESSION_TTL minus one —
    // a second's disagreement with Go on a header an operator can read.
    "rounds a part-second remainder up rather than truncating it" {
        withClue("one nanosecond short of a whole second") {
            maxAgeOf(now.plusSeconds(2_592_000).minusNanos(1)) shouldBe "2592000"
        }
        withClue("half a second short") {
            maxAgeOf(now.plusSeconds(30).minusMillis(500)) shouldBe "30"
        }
    }

    // Max-Age=0 does not mean "expires immediately", it means DELETE THIS
    // COOKIE — which is what clear() sends. A refresh racing its own expiry
    // must not accidentally send the client a logout, so Go floors at 1 and so
    // does this.
    "never issues a set-cookie with Max-Age 0" {
        withClue("expiry already passed") {
            maxAgeOf(now.minusSeconds(5)) shouldBe "1"
        }
        withClue("expiry passed by a fraction of a second") {
            maxAgeOf(now.minusMillis(500)) shouldBe "1"
        }
        withClue("expiry exactly now") {
            maxAgeOf(now) shouldBe "1"
        }
    }

    // The other half: clearing IS Max-Age=0, and Go's MaxAge:-1 renders to the
    // same header (TestClearedSessionCookieDeletesTheClientsCopy).
    "clears with Max-Age 0 and an empty value" {
        val header = factoryAt(now).clear()

        header.cookieAttributes()["max-age"] shouldBe "0"
        header.substringBefore(';') shouldBe "${SessionCookieFactory.COOKIE_NAME}="
    }
})
