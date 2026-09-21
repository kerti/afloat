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
        base(token, Duration.between(clock.instant(), expires).toSeconds().coerceAtLeast(0)).build().toString()

    fun clear(): String = base("", 0).build().toString()
}
