package dev.kerti.afloat.auth

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
})
