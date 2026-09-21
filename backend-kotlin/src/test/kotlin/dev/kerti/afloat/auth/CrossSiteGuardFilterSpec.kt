package dev.kerti.afloat.auth

import io.kotest.assertions.withClue
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe
import jakarta.servlet.http.HttpServlet
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.mock.web.MockFilterChain
import org.springframework.mock.web.MockHttpServletRequest
import org.springframework.mock.web.MockHttpServletResponse

// #14 tests 61-67. Sec-Fetch-Site is the browser's own statement and page
// script cannot set it; Origin is the fallback for clients that send neither.
class CrossSiteGuardFilterSpec : StringSpec({

    // Reached only when the filter passes the request on, so "did a handler
    // run" is an observation rather than an inference.
    class Outcome(val reached: Boolean, val response: MockHttpServletResponse)

    fun run(
        method: String = "POST",
        secFetchSite: String? = null,
        origin: String? = null,
        host: String? = "afloat.example",
        serverName: String = "afloat.example",
        serverPort: Int = 443,
    ): Outcome {
        val request = MockHttpServletRequest(method, "/api/auth/local/login").apply {
            this.serverName = serverName
            this.serverPort = serverPort
            secFetchSite?.let { addHeader("Sec-Fetch-Site", it) }
            origin?.let { addHeader("Origin", it) }
            host?.let { addHeader("Host", it) }
        }
        val response = MockHttpServletResponse()
        var reached = false
        val chain = MockFilterChain(object : HttpServlet() {
            override fun service(req: HttpServletRequest, res: HttpServletResponse) {
                reached = true
            }
        })

        CrossSiteGuardFilter().doFilter(request, response, chain)
        return Outcome(reached, response)
    }

    // crossSitePostIsRejectedWith403CrossSiteRequestBlocked
    "rejects a cross-site POST with 403 CROSS_SITE_REQUEST_BLOCKED" {
        val outcome = run(secFetchSite = "cross-site")

        outcome.response.status shouldBe 403
        outcome.response.contentAsString shouldBe """{"code":"CROSS_SITE_REQUEST_BLOCKED"}"""
        outcome.reached shouldBe false
    }

    // sameOriginPostIsAllowed
    "allows a same-origin POST" {
        val outcome = run(secFetchSite = "same-origin")

        outcome.reached shouldBe true
        outcome.response.status shouldBe 200
    }

    // secFetchSiteNoneIsAllowed
    "allows Sec-Fetch-Site: none" {
        // A direct navigation, or a tool with no originating site.
        run(secFetchSite = "none").reached shouldBe true
    }

    // secFetchSiteSameSiteIsRejected
    "rejects Sec-Fetch-Site: same-site" {
        // Same site, different origin: exactly what SameSite=Lax does NOT
        // cover, and the reason this guard exists at all.
        val outcome = run(secFetchSite = "same-site")

        outcome.response.status shouldBe 403
        outcome.reached shouldBe false
    }

    // originHeaderIsUsedWhenSecFetchSiteIsAbsent
    "falls back to Origin when Sec-Fetch-Site is absent" {
        withClue("matching host") {
            run(origin = "https://afloat.example").reached shouldBe true
        }
        withClue("mismatched host") {
            val outcome = run(origin = "https://evil.example")
            outcome.reached shouldBe false
            outcome.response.status shouldBe 403
        }
        withClue("unparseable Origin") {
            val outcome = run(origin = "://not a uri")
            outcome.reached shouldBe false
            outcome.response.status shouldBe 403
        }
        withClue("the literal Origin: null a sandboxed iframe sends") {
            // Parses as a relative URI with no authority, so it matches no
            // host. Worth pinning rather than inferring: `null` is the one
            // Origin value that is neither a real origin nor a parse failure,
            // and a guard that let it through would be trusting the one context
            // the browser is telling it not to.
            val outcome = run(origin = "null")
            outcome.reached shouldBe false
            outcome.response.status shouldBe 403
        }
    }

    // Go compares url.URL.Host, which splits userinfo off into url.URL.User;
    // URI.authority keeps it. Comparing authority rejected an Origin that Go
    // accepts — harmless in itself, since no browser puts userinfo in an
    // Origin, but a guard whose whole purpose is that both backends refuse the
    // same request is the wrong place for the two to disagree.
    "ignores userinfo in the Origin, as Go's url.Host does" {
        withClue("userinfo in front of a matching host") {
            run(origin = "https://someone@afloat.example").reached shouldBe true
        }
        withClue("userinfo in front of a mismatched host is still blocked") {
            run(origin = "https://afloat.example@evil.example").reached shouldBe false
        }
    }

    "compares Origin against the Host header including its port" {
        // Behind a proxy the request's own serverPort is the local one and
        // would never match a public 443, so the Host header is the comparison
        // whenever it is present - the same thing Go compares.
        withClue("host carries a port and Origin agrees") {
            run(origin = "https://afloat.example:8443", host = "afloat.example:8443").reached shouldBe true
        }
        withClue("host carries a port and Origin does not") {
            run(origin = "https://afloat.example", host = "afloat.example:8443").reached shouldBe false
        }
    }

    // requestWithNeitherHeaderIsAllowed
    "allows a request carrying neither header" {
        // Not a browser form post, so there is no ambient cookie to abuse.
        // curl and the test suite land here.
        run(secFetchSite = null, origin = null).reached shouldBe true
    }

    // safeMethodsAreNeverBlocked
    "never blocks a safe method" {
        listOf("GET", "HEAD", "OPTIONS", "TRACE").forEach { method ->
            withClue("$method with Sec-Fetch-Site: cross-site") {
                // Blocking a cross-site GET breaks ordinary navigation.
                run(method = method, secFetchSite = "cross-site").reached shouldBe true
            }
        }
    }

    "still blocks the other unsafe methods" {
        listOf("POST", "PUT", "PATCH", "DELETE").forEach { method ->
            withClue("$method with Sec-Fetch-Site: cross-site") {
                run(method = method, secFetchSite = "cross-site").reached shouldBe false
            }
        }
    }

    // No Host header: an HTTP/2 request, whose :authority the container surfaces
    // as serverName and serverPort. The fallback must compare the way the Host
    // branch does, so an Origin without a port names the scheme's default one.
    "without a Host header, allows an Origin on the request's own authority" {
        run(origin = "https://afloat.example", host = null).reached shouldBe true
        run(origin = "https://afloat.example:8443", host = null, serverPort = 8443).reached shouldBe true
    }

    "without a Host header, rejects an Origin with no port when the server is not on the default one" {
        // The bug: `uri.port == -1` matched any serverPort, so this passed.
        val outcome = run(origin = "https://afloat.example", host = null, serverPort = 8443)

        outcome.response.status shouldBe 403
        outcome.reached shouldBe false
    }

    "without a Host header, rejects another host or another port" {
        run(origin = "https://evil.example", host = null).reached shouldBe false
        run(origin = "https://afloat.example:8443", host = null).reached shouldBe false
        run(origin = "http://afloat.example", host = null).reached shouldBe false
    }
})
