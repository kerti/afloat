package dev.kerti.afloat.httperr

import dev.kerti.afloat.testsupport.DatabaseSpec
import io.kotest.assertions.withClue
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain
import jakarta.servlet.Filter
import org.springframework.boot.test.context.SpringBootTest
import org.springframework.boot.test.context.TestConfiguration
import org.springframework.boot.web.servlet.FilterRegistrationBean
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Import
import org.springframework.core.Ordered
import org.springframework.test.context.DynamicPropertyRegistry
import org.springframework.test.context.DynamicPropertySource
import java.net.ServerSocket
import java.net.Socket
import java.net.URI
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse

// What reaches the container's /error dispatch: a failure past
// DispatcherServlet, which ApiExceptionHandler never sees (#32 item 1), and a
// path the firewall refuses (#56). MockMvc makes neither - the firewall and
// the ERROR dispatch are the container's - so this runs a real Tomcat and sends
// it real requests, with a filter that throws on one path.
//
// DEFINED_PORT on a port found free, not RANDOM_PORT: that sets server.port to
// 0, which AppConfig refuses, as it would from an operator.
@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.DEFINED_PORT)
@Import(ErrorDispatchSpec.ThrowingFilter::class)
class ErrorDispatchSpec : DatabaseSpec() {

    companion object {
        private val port: Int = ServerSocket(0).use { it.localPort }

        @JvmStatic
        @DynamicPropertySource
        fun serverPort(registry: DynamicPropertyRegistry) {
            registry.add("PORT") { port }
        }
    }

    @TestConfiguration
    class ThrowingFilter {
        // After the security chain, so the request is past every filter that
        // writes its own envelope. It throws on the REQUEST dispatch only; the
        // ERROR dispatch that follows passes through it.
        @Bean
        fun boom() = FilterRegistrationBean(
            Filter { request, response, chain ->
                if ((request as jakarta.servlet.http.HttpServletRequest).requestURI == "/api/boom") {
                    throw IllegalStateException("a filter failed")
                }
                chain.doFilter(request, response)
            }
        ).apply { order = Ordered.LOWEST_PRECEDENCE }
    }

    private fun get(path: String, accept: String? = null): HttpResponse<String> {
        val request = HttpRequest.newBuilder(URI("http://localhost:$port$path")).GET()
        if (accept != null) request.header("Accept", accept)
        return HttpClient.newHttpClient().send(request.build(), HttpResponse.BodyHandlers.ofString())
    }

    private fun headersWithoutPerResponse(response: HttpResponse<String>) =
        response.headers().map().filterKeys { it.lowercase() !in setOf("date", "x-request-id") }

    init {
        "answers a filter that throws with the envelope, not Boot's error body" {
            val response = get("/api/boom")

            response.statusCode() shouldBe 500
            response.body() shouldBe """{"code":"INTERNAL"}"""
            response.headers().firstValue("Content-Type").orElse("") shouldBe "application/json"
            // The fixed set, as on any other response (#26): Go's
            // securityHeaders middleware is ahead of its recoverer.
            response.headers().firstValue("X-Content-Type-Options").orElse("") shouldBe "nosniff"
            response.headers().firstValue("Cache-Control").orElse("") shouldBe
                "no-cache, no-store, max-age=0, must-revalidate"
        }

        // #56: a path the firewall refuses is a path that does not exist, and
        // answers exactly as one does - status, every header but the
        // per-response ones, and the empty body. Only a real Tomcat runs the
        // firewall and the /error dispatch it leads to.
        "answers a path the firewall refuses exactly like an unregistered one" {
            val unregistered = get("/api/no-such-route")
            unregistered.statusCode() shouldBe 404
            listOf(
                "/api/health;x=1", "/api//health", "/api/./health", "/api/h%25ealth", "/api/auth%2Fmethods",
                "/api/health%5C", "/api/auth%5Cmethods",
                // Requested directly, /error is a path the API does not have.
                "/error",
            ).forEach { path ->
                val refused = get(path)
                withClue(path) {
                    refused.statusCode() shouldBe 404
                    refused.body() shouldBe ""
                    headersWithoutPerResponse(refused) shouldBe headersWithoutPerResponse(unregistered)
                }
            }
        }

        // The firewall still refuses an unknown method with a bare 400, never
        // the "no such path" 404 - #67 only relaxed header VALUES, not methods.
        "answers a request the firewall refuses for its method with a bare 400" {
            val unregistered = get("/api/no-such-route")
            val method = HttpClient.newHttpClient().send(
                HttpRequest.newBuilder(URI("http://localhost:$port/api/health"))
                    .method("FOO", HttpRequest.BodyPublishers.noBody()).build(),
                HttpResponse.BodyHandlers.ofString(),
            )
            withClue("FOO /api/health") {
                method.statusCode() shouldBe 400
                method.body() shouldBe ""
                // Tomcat closes the connection after any 400 and says so; that
                // is the connector's framing, not a header this app writes.
                headersWithoutPerResponse(method) - "connection" shouldBe headersWithoutPerResponse(unregistered)
            }
        }

        // #67: a header value holding a control character - Tomcat reads
        // header bytes as Latin-1, so 0x85 arrives as U+0085 - no longer trips
        // the firewall. This used to be a bare 400 (SecurityConfiguration's
        // StrictHttpFirewall default); now the request reaches the handler
        // like any other. Raw socket for the header, since java.net.http will
        // not send the byte.
        "accepts a header value holding a control character rather than refusing it" {
            val raw = Socket("localhost", port).use { socket ->
                socket.getOutputStream().write(
                    "GET /api/health HTTP/1.1\r\nHost: localhost\r\nUser-Agent: x".toByteArray() +
                        byteArrayOf(0x85.toByte()) + "y\r\nConnection: close\r\n\r\n".toByteArray(),
                )
                socket.getInputStream().readAllBytes().toString(Charsets.ISO_8859_1)
            }
            withClue(raw) {
                raw.lineSequence().first().trim() shouldBe "HTTP/1.1 200"
                raw.substringAfter("\r\n\r\n") shouldContain """{"status":"ok""""
            }

            // The same acceptance for a header first read inside MVC rather
            // than by a filter: Accept, read by content negotiation. Before
            // the firewall refused header values at all, this fell into
            // ApiExceptionHandler's catch-all as a 500; #67 leaves that risk
            // real again, so this pins that content negotiation itself
            // tolerates the byte rather than the firewall shielding it.
            val accept = Socket("localhost", port).use { socket ->
                socket.getOutputStream().write(
                    "GET /api/health HTTP/1.1\r\nHost: localhost\r\nAccept: application/json, x/".toByteArray() +
                        byteArrayOf(0x85.toByte()) + "\r\nConnection: close\r\n\r\n".toByteArray(),
                )
                socket.getInputStream().readAllBytes().toString(Charsets.ISO_8859_1)
            }
            withClue(accept) {
                accept.lineSequence().first().trim() shouldBe "HTTP/1.1 200"
                accept.substringAfter("\r\n\r\n") shouldContain """{"status":"ok""""
            }
        }

        // A browser asks for HTML. Boot's BasicErrorController has an HTML
        // handler for that; the API's content negotiation ignores Accept, so
        // the answer is the same JSON.
        "answers the same envelope when the client asks for HTML" {
            val response = get("/api/boom", accept = "text/html")

            response.statusCode() shouldBe 500
            response.body() shouldBe """{"code":"INTERNAL"}"""
        }
    }
}
