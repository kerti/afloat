package dev.kerti.afloat.httperr

import jakarta.servlet.FilterChain
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletRequestWrapper
import jakarta.servlet.http.HttpServletResponse
import org.springframework.http.HttpHeaders
import org.springframework.web.filter.OncePerRequestFilter
import java.util.Collections
import java.util.Enumeration

// A request body is UTF-8 whatever charset its Content-Type claims, as Go reads
// it (strictJSONBodyDecoder never consults the header): application/json has
// no charset parameter (RFC 8259 §11). So every charset parameter is rewritten
// to UTF-8 before Spring parses the header. Left alone, a known charset decodes
// the body through that charset, and a name the JDK does not know
// (charset=bogus) fails Spring's media type parsing, so the request matches no
// `consumes` and answers INVALID_JSON_BODY where Go validates the body.
//
// A JSON body keeps no parameter at all. Go ignores every one, where Spring
// refuses one it cannot parse (foo=a b, foo=, an unclosed quote) as it
// refused charset=bogus.
class Utf8CharsetFilter : OncePerRequestFilter() {
    override fun doFilterInternal(
        request: HttpServletRequest,
        response: HttpServletResponse,
        filterChain: FilterChain,
    ) {
        val contentType = request.contentType
        val relabelled = contentType?.let(::withUtf8Charset)
        filterChain.doFilter(
            if (relabelled == null || relabelled == contentType) request else Utf8Request(request, relabelled),
            response,
        )
    }
}

// Only the parameters are touched; the media type itself is left for Spring
// to judge.
internal fun withUtf8Charset(contentType: String): String {
    val parts = contentType.split(';')
    if (parts.first().trim(' ', '\t').equals("application/json", ignoreCase = true)) return parts.first()
    val params = parts.drop(1).map { param ->
        if (param.substringBefore('=').trim().equals("charset", ignoreCase = true)) "charset=UTF-8" else param
    }
    return (listOf(parts.first()) + params).joinToString(";")
}

private class Utf8Request(request: HttpServletRequest, private val contentType: String) : HttpServletRequestWrapper(request) {
    override fun getContentType(): String = contentType

    override fun getCharacterEncoding(): String = Charsets.UTF_8.name()

    override fun getHeader(name: String): String? =
        if (name.equals(HttpHeaders.CONTENT_TYPE, ignoreCase = true)) contentType else super.getHeader(name)

    override fun getHeaders(name: String): Enumeration<String> =
        if (name.equals(HttpHeaders.CONTENT_TYPE, ignoreCase = true)) {
            Collections.enumeration(listOf(contentType))
        } else {
            super.getHeaders(name)
        }
}
