package dev.kerti.afloat.auth

import io.kotest.assertions.withClue
import io.kotest.core.spec.style.StringSpec
import io.kotest.datatest.withData
import io.kotest.matchers.ints.shouldBeLessThanOrEqual
import io.kotest.matchers.nulls.shouldBeNull
import io.kotest.matchers.shouldBe
import io.kotest.matchers.shouldNotBe
import jakarta.servlet.http.Cookie
import jakarta.servlet.http.HttpServlet
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.mock.web.MockFilterChain
import org.springframework.mock.web.MockHttpServletRequest
import org.springframework.mock.web.MockHttpServletResponse

class RequestFactsFilterSpec : StringSpec({

    "carries ip, user agent and session token into the context" {
        val request = MockHttpServletRequest().apply {
            remoteAddr = "203.0.113.7"
            addHeader("User-Agent", "afloat-test/1.0")
            setCookies(Cookie(SessionCookieFactory.COOKIE_NAME, "token-abc"))
        }
        var seen: RequestFacts? = null
        val chain = MockFilterChain(object : HttpServlet() {
            override fun service(
                req: HttpServletRequest,
                res: HttpServletResponse,
            ) {
                seen = RequestContext.current()
            }
        })

        RequestFactsFilter().doFilter(request, MockHttpServletResponse(), chain)

        seen shouldBe RequestFacts("203.0.113.7", "afloat-test/1.0", "token-abc")
    }

    // sessions.user_agent is one column both backends write. Go stores an
    // empty header as NULL, so an empty header must not arrive here as ''
    // (#32 item 4). The harness cannot send one: net/http drops an empty
    // User-Agent rather than sending it.
    "carries an empty user agent as none at all" {
        val request = MockHttpServletRequest().apply {
            remoteAddr = "203.0.113.7"
            addHeader("User-Agent", "")
        }
        var seen: RequestFacts? = null
        val chain = MockFilterChain(object : HttpServlet() {
            override fun service(
                req: HttpServletRequest,
                res: HttpServletResponse,
            ) {
                seen = RequestContext.current()
            }
        })

        RequestFactsFilter().doFilter(request, MockHttpServletResponse(), chain)

        withClue("facts: $seen") { seen?.userAgent.shouldBeNull() }
    }

    "clears the context once the request is done" {
        val request = MockHttpServletRequest().apply { remoteAddr = "203.0.113.7" }

        RequestFactsFilter().doFilter(request, MockHttpServletResponse(), MockFilterChain())

        // A pooled thread that kept the facts would hand one household's
        // session token to the next request on that thread.
        RequestContext.current().shouldBeNull()
    }

    "carries no token when the request has no session cookie" {
        val request = MockHttpServletRequest().apply { remoteAddr = "203.0.113.7" }
        var seen: RequestFacts? = null
        val chain = MockFilterChain(object : HttpServlet() {
            override fun service(
                req: HttpServletRequest,
                res: HttpServletResponse,
            ) {
                seen = RequestContext.current()
            }
        })

        RequestFactsFilter().doFilter(request, MockHttpServletResponse(), chain)

        seen?.sessionToken.shouldBeNull()
    }

    // Go writes `ip:::1`; Tomcat reports the expanded form. Both backends share
    // login_attempts, so the spelling has to agree or one address gets two rows.
    "spells an IPv6 remote address the way Go does" {
        mapOf(
            "0:0:0:0:0:0:0:1" to "::1",
            "0:0:0:0:0:0:0:0" to "::",
            "2001:db8:0:0:0:0:2:1" to "2001:db8::2:1",
            "2001:0db8:0000:0000:0001:0000:0000:0001" to "2001:db8::1:0:0:1",
            "2001:db8:0:1:1:1:1:1" to "2001:db8:0:1:1:1:1:1",
            "FE80:0:0:0:0:0:0:1" to "fe80::1",
            "fe80:0:0:0:0:0:0:1%eth0" to "fe80::1%eth0",
            "0:0:0:0:0:ffff:c000:280" to "192.0.2.128",
            "203.0.113.7" to "203.0.113.7",
            "not an address" to "not an address",
            "" to "",
        ).forEach { (raw, expected) ->
            withClue(raw) { normalizeIp(raw) shouldBe expected }
        }
    }

    // #67: Tomcat decodes header bytes as ISO-8859-1, so a real connector
    // hands the filter the mis-decoded String this constructs by hand - the
    // same three chars "Afloat — test"'s UTF-8 bytes (E2 80 94, the em dash)
    // become when each is read back as its own Latin-1 codepoint. Without
    // sanitizeUserAgent's decode step, sessions.user_agent would hold
    // different bytes for the same wire bytes than Go's r.UserAgent(), which
    // carries them through untouched.
    "recovers a UTF-8 User-Agent Tomcat mis-decoded as ISO-8859-1" {
        val wireBytes = "Afloat — test".toByteArray(Charsets.UTF_8)
        val asTomcatDeliversIt = String(wireBytes, Charsets.ISO_8859_1)
        asTomcatDeliversIt shouldNotBe "Afloat — test" // the fixture is genuinely mis-decoded, or this proves nothing

        val request = MockHttpServletRequest().apply {
            remoteAddr = "203.0.113.7"
            addHeader("User-Agent", asTomcatDeliversIt)
        }
        var seen: RequestFacts? = null
        val chain = MockFilterChain(object : HttpServlet() {
            override fun service(req: HttpServletRequest, res: HttpServletResponse) {
                seen = RequestContext.current()
            }
        })

        RequestFactsFilter().doFilter(request, MockHttpServletResponse(), chain)

        seen?.userAgent shouldBe "Afloat — test"
    }

    // R1: the storage rule for sessions.user_agent, in order - absent/empty is
    // null (#32 item 4, unchanged), not-valid-UTF-8 is null (a stored invalid
    // header used to fail the whole login with a Postgres 500 on Go, and
    // silently stored U+FFFD mojibake here before this rule), otherwise
    // truncated to 512 Unicode code points on a code point boundary. Go's
    // request_test.go holds the same rows. Each `wire` is the exact bytes a
    // client sent; asTomcatDeliversIt reproduces what Tomcat's ISO-8859-1
    // header decoding hands the filter for those bytes.
    fun asTomcatDeliversIt(wire: ByteArray): String = String(wire, Charsets.ISO_8859_1)

    data class UserAgentCase(val name: String, val wire: ByteArray, val want: String?)

    withData(
        nameFn = { it.name },
        listOf(
            UserAgentCase("ordinary ASCII passes through", "conformance/1.0".toByteArray(Charsets.US_ASCII), "conformance/1.0"),
            UserAgentCase("valid multibyte UTF-8 passes through", "Afloat — test 🔐".toByteArray(Charsets.UTF_8), "Afloat — test 🔐"),
            UserAgentCase("lone continuation byte 0x85 is invalid UTF-8", byteArrayOf('x'.code.toByte(), 0x85.toByte(), 'y'.code.toByte()), null),
            UserAgentCase("overlong encoding C0 AF is invalid UTF-8", byteArrayOf('x'.code.toByte(), 0xc0.toByte(), 0xaf.toByte(), 'y'.code.toByte()), null),
            UserAgentCase("a truncated 3-byte sequence is invalid UTF-8", byteArrayOf('x'.code.toByte(), 0xe2.toByte(), 0x80.toByte(), 'y'.code.toByte()), null),
            UserAgentCase("a lone 0xFF is invalid UTF-8", byteArrayOf('x'.code.toByte(), 0xff.toByte(), 'y'.code.toByte()), null),
            UserAgentCase("Latin-1 'café' (not UTF-8) is invalid UTF-8", byteArrayOf('c'.code.toByte(), 'a'.code.toByte(), 'f'.code.toByte(), 0xe9.toByte()), null),
            UserAgentCase(
                "valid text with one invalid byte is invalid UTF-8 as a whole",
                byteArrayOf('a'.code.toByte(), 'b'.code.toByte(), 'c'.code.toByte(), 0x85.toByte(), 'd'.code.toByte(), 'e'.code.toByte(), 'f'.code.toByte()),
                null,
            ),
            UserAgentCase("511 code points is untouched", "a".repeat(511).toByteArray(Charsets.US_ASCII), "a".repeat(511)),
            UserAgentCase(
                "512 code points, multibyte at the boundary, is untouched",
                ("a".repeat(511) + "💚").toByteArray(Charsets.UTF_8),
                "a".repeat(511) + "💚",
            ),
            UserAgentCase(
                "513 code points, multibyte astride the cut, truncates to 512 without splitting it",
                ("a".repeat(511) + "💚" + "a").toByteArray(Charsets.UTF_8),
                "a".repeat(511) + "💚",
            ),
        ),
    ) { case ->
        val got = sanitizeUserAgent(asTomcatDeliversIt(case.wire))
        got shouldBe case.want
        if (got != null) {
            got.codePointCount(0, got.length) shouldBeLessThanOrEqual 512
        }
    }

    "sanitizeUserAgent treats an absent header as null" {
        sanitizeUserAgent(null) shouldBe null
    }

    "sanitizeUserAgent treats an empty header as null" {
        sanitizeUserAgent("") shouldBe null
    }

    "carries the normalised address into the context" {
        val request = MockHttpServletRequest().apply { remoteAddr = "0:0:0:0:0:0:0:1" }
        var seen: RequestFacts? = null
        val chain = MockFilterChain(object : HttpServlet() {
            override fun service(req: HttpServletRequest, res: HttpServletResponse) {
                seen = RequestContext.current()
            }
        })

        RequestFactsFilter().doFilter(request, MockHttpServletResponse(), chain)

        seen?.clientIp shouldBe "::1"
    }
})
