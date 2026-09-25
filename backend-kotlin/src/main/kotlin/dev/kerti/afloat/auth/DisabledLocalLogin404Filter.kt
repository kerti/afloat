package dev.kerti.afloat.auth

import jakarta.servlet.FilterChain
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.web.filter.OncePerRequestFilter
import org.springframework.web.util.UrlPathHelper

// Kotlin's mirror of Go's disabledLocalLogin404 (issue #24): with the local
// provider off, /api/auth/local/login must answer the SAME bare 404 an
// unmatched path does — status 404, empty body — on every method, and no
// session row may be written. A gate keyed on method would let chi (Go) /
// Spring (Kotlin) answer a wrong-method request with 405, which discloses
// that the route exists (Go #47).
class DisabledLocalLogin404Filter(
    private val enabled: Boolean,
    private val loginPath: String,
) : OncePerRequestFilter() {
    // The decoded path, the one Spring routes on - never requestURI, which is
    // still percent-encoded: compared raw, /api/auth/local/%6Cogin passed the
    // gate and Spring served it as login, so a disabled provider signed users
    // in (#56). Go's gate reads the decoded r.URL.Path for the same reason.
    override fun shouldNotFilter(request: HttpServletRequest): Boolean =
        enabled || UrlPathHelper.defaultInstance.getPathWithinApplication(request) != loginPath

    override fun doFilterInternal(
        request: HttpServletRequest,
        response: HttpServletResponse,
        filterChain: FilterChain
    ) {
        // Matched on path alone, deliberately: 405 tells a prober the route
        // exists. This writes the same bytes Spring answers for a genuinely
        // unregistered path — no envelope, no Content-Type.
        response.status = HttpServletResponse.SC_NOT_FOUND
    }
}
