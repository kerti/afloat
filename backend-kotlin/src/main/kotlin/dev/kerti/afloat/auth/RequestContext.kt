package dev.kerti.afloat.auth

import jakarta.servlet.FilterChain
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.http.HttpHeaders
import org.springframework.web.filter.OncePerRequestFilter
import java.nio.ByteBuffer
import java.nio.charset.CharacterCodingException
import java.nio.charset.CodingErrorAction

data class RequestFacts(
    val clientIp: String,
    val userAgent: String?,
    val sessionToken: String?,
)

// sessions.user_agent's storage rule, in order (R1, on top of #32 item 4's
// original empty-is-NULL rule):
//
// 1. Absent or empty: null.
// 2. Not valid UTF-8: null. Tomcat decodes every header VALUE's bytes as
//    ISO-8859-1 - HTTP/1.1's own field-content is undefined past US-ASCII
//    (RFC 7230 §3.2, obs-text), and Java's Http11InputBuffer picks Latin-1, a
//    lossless 1-byte-to-1-char mapping, as its reading of that undefined
//    range - so recovering what the client actually sent means re-encoding
//    Tomcat's String back to those bytes and decoding THEM as UTF-8, with a
//    STRICT decoder (CodingErrorAction.REPORT): the previous version used
//    String(bytes, UTF_8), which silently replaces an invalid sequence with
//    U+FFFD instead of refusing it, so a client that sent invalid UTF-8 (Go's
//    r.UserAgent() carries the raw bytes through untouched, and a Postgres
//    TEXT column is UTF8-encoded, so THAT used to fail Go's login outright
//    with a 500) was quietly stored as mojibake here rather than as nothing,
//    which is what Go effectively does by refusing the whole request.
// 3. Otherwise, truncated to MAX_USER_AGENT_CODE_POINTS Unicode code points -
//    never bytes, never UTF-16 units, matching every other length limit in
//    this codebase (BOOTSTRAP.md §5.1) - on a code point boundary via
//    offsetByCodePoints, which can only ever land between two of them.
private val MAX_USER_AGENT_CODE_POINTS = 512

internal fun sanitizeUserAgent(headerValue: String?): String? {
    if (headerValue.isNullOrEmpty()) return null
    val decoder = Charsets.UTF_8.newDecoder()
        .onMalformedInput(CodingErrorAction.REPORT)
        .onUnmappableCharacter(CodingErrorAction.REPORT)
    val decoded = try {
        decoder.decode(ByteBuffer.wrap(headerValue.toByteArray(Charsets.ISO_8859_1))).toString()
    } catch (e: CharacterCodingException) {
        return null
    }
    if (decoded.isEmpty()) return null
    val codePointCount = decoded.codePointCount(0, decoded.length)
    if (codePointCount <= MAX_USER_AGENT_CODE_POINTS) return decoded
    val cut = decoded.offsetByCodePoints(0, MAX_USER_AGENT_CODE_POINTS)
    return decoded.substring(0, cut)
}

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
                    clientIp = normalizeIp(request.remoteAddr),
                    // Absent, empty, invalid UTF-8, or over 512 code points:
                    // sanitizeUserAgent's rule (R1, #32 item 4).
                    userAgent = sanitizeUserAgent(request.getHeader(HttpHeaders.USER_AGENT)),
                    sessionToken = token,
                )
            )
            filterChain.doFilter(request, response)
        } finally {
            RequestContext.clear()
        }
    }
}

// Go keys the backoff on net.SplitHostPort(RemoteAddr), whose IPv6 spelling is
// the compressed RFC 5952 one (`::1`); Tomcat reports the expanded
// `0:0:0:0:0:0:0:1`. The same address must land on the same `ip:` row in both
// backends, since they share the table. IPv4 and anything that is not an IP
// literal pass through untouched.
internal fun normalizeIp(remoteAddr: String?): String {
    val raw = remoteAddr?.trim().orEmpty()
    if (':' !in raw) return raw
    val zoneAt = raw.indexOf('%')
    val literal = if (zoneAt >= 0) raw.substring(0, zoneAt) else raw
    val zone = if (zoneAt >= 0) raw.substring(zoneAt) else ""
    // Literal-only parse: a string that is not an address never reaches DNS.
    if (!literal.all { it.isDigit() || it in 'a'..'f' || it in 'A'..'F' || it == ':' || it == '.' }) return raw
    val address = try {
        java.net.InetAddress.getByName(literal)
    } catch (e: java.net.UnknownHostException) {
        return raw
    }
    // An IPv4-mapped address comes back as Inet4Address, spelled dotted, as Go does.
    if (address !is java.net.Inet6Address) return address.hostAddress
    val bytes = address.address
    val groups = IntArray(8) { ((bytes[2 * it].toInt() and 0xff) shl 8) or (bytes[2 * it + 1].toInt() and 0xff) }
    // The longest run of zero groups collapses to `::`, the first on a tie, and
    // a run of one group is left alone (RFC 5952 §4.2).
    var bestStart = -1
    var bestLen = 0
    var i = 0
    while (i < 8) {
        if (groups[i] != 0) { i++; continue }
        var j = i
        while (j < 8 && groups[j] == 0) j++
        if (j - i > bestLen) { bestStart = i; bestLen = j - i }
        i = j
    }
    if (bestLen < 2) return groups.joinToString(":") { it.toString(16) } + zone
    val head = groups.take(bestStart).joinToString(":") { it.toString(16) }
    val tail = groups.drop(bestStart + bestLen).joinToString(":") { it.toString(16) }
    return "$head::$tail$zone"
}
