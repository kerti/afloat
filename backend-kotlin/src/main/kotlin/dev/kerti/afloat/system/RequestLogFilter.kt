package dev.kerti.afloat.system

import jakarta.servlet.FilterChain
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.slf4j.LoggerFactory
import org.springframework.boot.web.servlet.FilterRegistrationBean
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import org.springframework.core.Ordered
import org.springframework.web.filter.OncePerRequestFilter
import java.security.SecureRandom
import java.util.HexFormat
import java.util.concurrent.TimeUnit

// Method, path, status, duration and request id — and nothing else.
//
// Never the body, never query values, never the session cookie. PRD N6 forbids
// telemetry of any kind, and a log line carrying an Expense description is
// telemetry that happens to be written to disk. Go's requestLogger
// (backend/internal/httpserver/middleware.go) logs exactly these five, and
// docs/adr/go/0003 has the reasoning.
//
// requestURI, not the full URL: the query string never appears, which is half
// of how the "nothing else" above stays true.
class RequestLogFilter : OncePerRequestFilter() {

    override fun doFilterInternal(
        request: HttpServletRequest,
        response: HttpServletResponse,
        filterChain: FilterChain,
    ) {
        val requestId = requestIdFor(request)
        response.setHeader(REQUEST_ID_HEADER, requestId)
        val startedAt = System.nanoTime()
        try {
            filterChain.doFilter(request, response)
        } finally {
            // In a finally: a request that fails still gets its line, and the
            // status is whatever actually went out.
            log.info(
                "request method={} path={} status={} duration_ms={} request_id={}",
                request.method,
                request.requestURI,
                response.status,
                TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - startedAt),
                requestId,
            )
        }
    }

    // An inbound X-Request-Id is honoured so a reverse proxy's id survives into
    // these lines, matching chi's middleware.RequestID on the Go side.
    //
    // SANITISED, which chi does not do: the header is attacker-controlled in a
    // self-hosted deployment with nothing in front, and a value containing a
    // newline writes an attacker-chosen second line into the log. Restricting
    // it to the id alphabet and a length makes the worst case a useless id
    // rather than a forged record.
    private fun requestIdFor(request: HttpServletRequest): String {
        val supplied = request.getHeader(REQUEST_ID_HEADER)?.filter { it.isLetterOrDigit() || it == '-' || it == '_' }
        return if (!supplied.isNullOrEmpty()) supplied.take(MAX_REQUEST_ID_LENGTH) else newRequestId()
    }

    private fun newRequestId(): String {
        val bytes = ByteArray(8)
        random.nextBytes(bytes)
        return HexFormat.of().formatHex(bytes)
    }

    companion object {
        const val REQUEST_ID_HEADER = "X-Request-Id"
        private const val MAX_REQUEST_ID_LENGTH = 64

        private val random = SecureRandom()
        private val log = LoggerFactory.getLogger(RequestLogFilter::class.java)
    }
}

@Configuration
class RequestLogConfiguration {

    // Registered as a servlet filter rather than a link in the security chain,
    // so it wraps FilterChainProxy the way Go's requestLogger wraps everything
    // below it: a request refused by the cross-site guard is still a request,
    // and a log that starts after the guard cannot show it being refused.
    @Bean
    fun requestLogFilter(): FilterRegistrationBean<RequestLogFilter> =
        FilterRegistrationBean(RequestLogFilter()).apply {
            order = Ordered.HIGHEST_PRECEDENCE + 10
        }
}
