package dev.kerti.afloat.security

import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.security.web.header.HeaderWriter
import org.springframework.security.web.header.writers.CacheControlHeadersWriter
import org.springframework.security.web.header.writers.XContentTypeOptionsHeaderWriter
import org.springframework.security.web.header.writers.XXssProtectionHeaderWriter
import org.springframework.security.web.header.writers.frameoptions.XFrameOptionsHeaderWriter

// The fixed response header set (#26, BOOTSTRAP.md §5.2): Spring Security's
// defaults less HSTS, which the TLS terminator owns. One list, because two
// kinds of response are written outside the security chain's HeaderWriterFilter
// - a path the firewall refuses (#56), and the container's /error dispatch,
// which OncePerRequestFilter skips - and Go's securityHeaders middleware puts
// the set on every response it writes. The chain is built from this list too,
// so the three cannot drift apart.
object SecurityHeaders {
    val writers: List<HeaderWriter> = listOf(
        CacheControlHeadersWriter(),
        XContentTypeOptionsHeaderWriter(),
        XFrameOptionsHeaderWriter(XFrameOptionsHeaderWriter.XFrameOptionsMode.DENY),
        XXssProtectionHeaderWriter(),
    )

    fun write(request: HttpServletRequest, response: HttpServletResponse) =
        writers.forEach { it.writeHeaders(request, response) }
}
