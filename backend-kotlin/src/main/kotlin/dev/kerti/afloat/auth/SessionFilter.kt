package dev.kerti.afloat.auth

import dev.kerti.afloat.auth.data.Session
import dev.kerti.afloat.auth.data.SessionRepository
import dev.kerti.afloat.auth.data.UserRepository
import dev.kerti.afloat.config.AppConfig
import jakarta.servlet.FilterChain
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.slf4j.LoggerFactory
import org.springframework.dao.DataAccessException
import org.springframework.http.HttpHeaders
import org.springframework.security.authentication.UsernamePasswordAuthenticationToken
import org.springframework.security.core.context.SecurityContextHolder
import org.springframework.web.filter.OncePerRequestFilter
import java.time.Clock
import java.time.Duration
import java.time.Instant

// Resolves the session cookie into an authenticated principal. Never rejects:
// an invalid session leaves the request unauthenticated, so a public route
// still works and the authorization filter turns /me into a 401 envelope
// through the entry point. Mirrors Go's SessionMiddleware + RequireAuth split.
class SessionFilter(
    private val sessionRepository: SessionRepository,
    private val userRepository: UserRepository,
    private val cookieFactory: SessionCookieFactory,
    private val clock: Clock,
    private val appConfig: AppConfig,
) : OncePerRequestFilter() {
    override fun doFilterInternal(
        request: HttpServletRequest,
        response: HttpServletResponse,
        filterChain: FilterChain,
    ) {
        val token = RequestContext.current()?.sessionToken
        if (token.isNullOrBlank()) {
            filterChain.doFilter(request, response)
            return
        }
        try {
            val now = clock.instant()
            val session = try {
                sessionRepository.findLive(
                    TokenService.hash(token),
                    now,
                    now.minus(appConfig.sessionMaxLifetime),
                )
            } catch (e: DataAccessException) {
                // A lookup that failed says nothing about the session, so the
                // cookie is left alone and the request continues
                // unauthenticated (Go's session.go:131-138).
                log.error("session lookup", e)
                filterChain.doFilter(request, response)
                return
            }
            if (session == null) {
                // Expired, revoked, or never existed: stop the browser presenting
                // a cookie that cannot work.
                response.addHeader(HttpHeaders.SET_COOKIE, cookieFactory.clear())
                filterChain.doFilter(request, response)
                return
            }
            val user = try {
                userRepository.findById(session.userId).orElse(null)
            } catch (e: DataAccessException) {
                log.error("session: look up user", e)
                filterChain.doFilter(request, response)
                return
            }
            if (user == null) {
                // The session outlived its User — soft-deleted, most likely.
                response.addHeader(HttpHeaders.SET_COOKIE, cookieFactory.clear())
                filterChain.doFilter(request, response)
                return
            }

            touchIfStale(response, session, token, now)

            val authentication = UsernamePasswordAuthenticationToken(
                AuthenticatedUser(user.id, user.householdId, user.email),
                null,
                emptyList(),
            )
            SecurityContextHolder.getContext().authentication = authentication
            filterChain.doFilter(request, response)
        } finally {
            SecurityContextHolder.clearContext()
        }
    }

    // Extends the sliding window, but only once the session is past half its
    // life: otherwise every GET is a write on the one table read by every
    // request (Go's touchIfStale).
    private fun touchIfStale(
        response: HttpServletResponse,
        session: Session,
        token: String,
        now: Instant,
    ) {
        val remaining = Duration.between(now, session.expiresAt)
        if (remaining > appConfig.sessionTtl.dividedBy(2)) return
        val newExpiry = now.plus(appConfig.sessionTtl)
        try {
            sessionRepository.touch(session.id, now, newExpiry)
        } catch (e: DataAccessException) {
            // A failed refresh is not worth failing the request: the session is
            // still valid until its current expiry.
            log.warn("touch session", e)
            return
        }
        // The cookie must keep the PLAINTEXT token the client presented, or
        // the next request's hash never matches this row.
        response.addHeader(HttpHeaders.SET_COOKIE, cookieFactory.set(token, newExpiry))
    }

    companion object {
        private val log = LoggerFactory.getLogger(SessionFilter::class.java)
    }
}
