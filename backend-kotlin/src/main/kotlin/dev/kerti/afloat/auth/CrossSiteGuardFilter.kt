package dev.kerti.afloat.auth

import dev.kerti.afloat.api.model.ErrorCode
import dev.kerti.afloat.httperr.ApiErrorWriter
import jakarta.servlet.FilterChain
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.http.HttpHeaders
import org.springframework.web.filter.OncePerRequestFilter
import java.net.URI

class CrossSiteGuardFilter : OncePerRequestFilter() {
    override fun doFilterInternal(
        request: HttpServletRequest,
        response: HttpServletResponse,
        filterChain: FilterChain,
    ) {
        if (isSafeMethod(request.method)) {
            filterChain.doFilter(request, response)
            return
        }

        // Sec-Fetch-Site is the browser's own statement and cannot be set by
        // page script. Preferred when present; `none` is a direct navigation or
        // a tool with no originating site.
        val site = request.getHeader("Sec-Fetch-Site")
        if (!site.isNullOrEmpty()) {
            if (site != "same-origin" && site != "none") {
                ApiErrorWriter.write(response, 403, ErrorCode.CROSS_SITE_REQUEST_BLOCKED)
                return
            }
            filterChain.doFilter(request, response)
            return
        }

        // Fall back to Origin for clients that send no Sec-Fetch-Site.
        val origin = request.getHeader("Origin")
        if (!origin.isNullOrEmpty()) {
            if (!isSameOrigin(origin, request)) {
                ApiErrorWriter.write(response, 403, ErrorCode.CROSS_SITE_REQUEST_BLOCKED)
                return
            }
        }

        // Neither header present: not a browser form post, so there is no
        // ambient cookie to abuse. curl and the test suite land here.
        filterChain.doFilter(request, response)
    }

    // Go compares url.Host to r.Host, both of which carry the port when the
    // client sent one, so the Host header is the comparison whenever it is
    // there — including behind a proxy, where the request's own serverPort is
    // the local one and would never match a public 443.
    private fun isSameOrigin(origin: String, request: HttpServletRequest): Boolean = try {
        val uri = URI.create(origin)
        val host = request.getHeader(HttpHeaders.HOST)
        if (host != null) {
            uri.authority == host
        } else {
            // HTTP/2 sends :authority and no Host header; the container
            // surfaces it as the server name and port instead.
            uri.host == request.serverName && (uri.port == -1 || uri.port == request.serverPort)
        }
    } catch (e: IllegalArgumentException) {
        false
    }

    private fun isSafeMethod(method: String): Boolean =
        method == "GET" || method == "HEAD" || method == "OPTIONS" || method == "TRACE"
}
