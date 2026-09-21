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
})
