package dev.kerti.afloat.auth

import jakarta.servlet.FilterChain
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.http.HttpHeaders
import org.springframework.web.filter.OncePerRequestFilter

data class RequestFacts(
    val clientIp: String,
    val userAgent: String?,
    val sessionToken: String?,
)

// The generated controllers carry no HttpServletRequest, so the few
// request-scoped facts authentication needs live here, populated by the filter
// below (the Kotlin mirror of Go's RequestContextMiddleware).
object RequestContext {
    private val threadLocal = ThreadLocal<RequestFacts?>()

    fun current(): RequestFacts? = threadLocal.get()
    fun set(facts: RequestFacts?) = threadLocal.set(facts)
    fun clear() = threadLocal.remove()
}

class RequestFactsFilter : OncePerRequestFilter() {
    // The facts live in a ThreadLocal that the finally below clears when the
    // initial dispatch returns. An async dispatch resumes on another thread
    // afterward, so it must re-populate them or the handler sees null.
    override fun shouldNotFilterAsyncDispatch(): Boolean = false

    override fun doFilterInternal(
        request: HttpServletRequest,
        response: HttpServletResponse,
        filterChain: FilterChain
    ) {
        val token = request.cookies?.firstOrNull { it.name == SessionCookieFactory.COOKIE_NAME }?.value
        try {
            // request.remoteAddr is the socket peer, never X-Forwarded-For: that
            // header is attacker-controlled so a self-hosted box would let an
            // attacker pick a fresh key per request (Go's clientIP is identical).
            RequestContext.set(
                RequestFacts(
                    clientIp = request.remoteAddr ?: "",
                    userAgent = request.getHeader(HttpHeaders.USER_AGENT),
                    sessionToken = token,
                )
            )
            filterChain.doFilter(request, response)
        } finally {
            RequestContext.clear()
        }
    }
}
