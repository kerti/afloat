package dev.kerti.afloat.auth

import jakarta.servlet.FilterChain
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.web.filter.OncePerRequestFilter

// Every OPTIONS request answers a flat 405: empty body, no Allow header
// naming the route's methods (#66). Afloat is same-origin with no CORS, so no
// client sends OPTIONS, and answering it identically on every path -
// registered, unregistered, or the disabled-login route - is what keeps a
// disabled route indistinguishable from one never registered at all (#24's
// rule "on every method"), without a second gate that could drift from
// DisabledLocalLogin404Filter's. Go's optionsRefused answers the same bytes.
//
// Placed ahead of DisabledLocalLogin404Filter (SecurityConfiguration), so an
// OPTIONS to the login route never reaches that gate at all rather than
// depending on it to also cover this method.
class OptionsRefusedFilter : OncePerRequestFilter() {
    override fun shouldNotFilter(request: HttpServletRequest): Boolean =
        request.method != "OPTIONS"

    override fun doFilterInternal(
        request: HttpServletRequest,
        response: HttpServletResponse,
        filterChain: FilterChain,
    ) {
        response.status = HttpServletResponse.SC_METHOD_NOT_ALLOWED
    }
}
