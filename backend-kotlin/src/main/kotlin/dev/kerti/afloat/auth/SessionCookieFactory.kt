package dev.kerti.afloat.auth

import dev.kerti.afloat.config.AppConfig
import org.springframework.http.ResponseCookie
import org.springframework.stereotype.Component
import java.time.Clock
import java.time.Duration
import java.time.Instant

@Component
class SessionCookieFactory(appConfig: AppConfig, private val clock: Clock) {
    companion object {
        // Deliberately not Balances' bare 'session': host-only cookies do not
        // collide across hostnames, but a self-hoster behind one hostname would
        // have them silently overwrite each other (BOOTSTRAP.md §5.2).
        const val COOKIE_NAME = "afloat_session"
    }

    private val secure = appConfig.cookieSecure

    private fun base(token: String, maxAgeSeconds: Long): ResponseCookie.ResponseCookieBuilder = ResponseCookie
        .from(COOKIE_NAME, token)
        .path("/")
        .httpOnly(true)
        .secure(secure)
        .sameSite("Lax")
        .maxAge(maxAgeSeconds)

    // The same Clock the callers computed `expires` from: a fixed clock in a
    // test would otherwise measure Max-Age against wall time.
    fun set(token: String, expires: Instant): String =
        base(token, maxAgeSeconds(expires)).build().toString()

    // Rounded UP, with a floor of 1, matching Go's
    // `max(int(math.Ceil(expires.Sub(h.now()).Seconds())), 1)`
    // (backend/internal/auth/session.go). Both halves matter and neither is
    // cosmetic:
    //
    //   - Ceil, because the caller reads the clock to compute `expires` and this
    //     reads it again a few microseconds later. Truncating turns the whole
    //     SESSION_TTL into TTL-1 on every single login, so the two backends
    //     would disagree by a second on a header the operator can read.
    //   - Floor of 1, because Max-Age=0 is not "expires immediately", it is
    //     DELETE THIS COOKIE. A refresh racing its own expiry would hand the
    //     browser a logout instead of a session.
    private fun maxAgeSeconds(expires: Instant): Long {
        val remaining = Duration.between(clock.instant(), expires)
        // Duration normalises `nano` into 0..999_999_999 with a possibly
        // negative `seconds`, so this is a true ceiling on either side of zero.
        val ceiled = remaining.seconds + if (remaining.nano > 0) 1 else 0
        return ceiled.coerceAtLeast(1)
    }

    fun clear(): String = base("", 0).build().toString()
}
