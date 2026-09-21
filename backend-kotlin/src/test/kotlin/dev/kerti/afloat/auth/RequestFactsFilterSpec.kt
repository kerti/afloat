package dev.kerti.afloat.auth

import io.kotest.assertions.withClue
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.nulls.shouldBeNull
import io.kotest.matchers.shouldBe
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
