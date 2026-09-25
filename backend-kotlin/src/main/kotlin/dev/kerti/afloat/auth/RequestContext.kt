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
                    clientIp = normalizeIp(request.remoteAddr),
                    // An empty header is no user agent: NULL in sessions.user_agent,
                    // as Go's nullString writes it, never '' (#32 item 4).
                    userAgent = request.getHeader(HttpHeaders.USER_AGENT)?.ifEmpty { null },
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
