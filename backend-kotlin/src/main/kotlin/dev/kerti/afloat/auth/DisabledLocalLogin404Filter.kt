package dev.kerti.afloat.auth

import jakarta.servlet.FilterChain
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.web.filter.OncePerRequestFilter

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
    override fun shouldNotFilter(request: HttpServletRequest): Boolean =
        enabled || request.requestURI != loginPath

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
