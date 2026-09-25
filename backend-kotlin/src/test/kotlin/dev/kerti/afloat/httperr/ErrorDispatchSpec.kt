package dev.kerti.afloat.httperr

import dev.kerti.afloat.testsupport.DatabaseSpec
import io.kotest.matchers.shouldBe
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
import java.net.URI
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse

// A failure past DispatcherServlet reaches Boot's /error, not
// ApiExceptionHandler (#32 item 1). MockMvc never makes that ERROR dispatch -
// it is the servlet container's - so this runs a real Tomcat and sends it real
// requests, with a filter that throws on one path.
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

    init {
        "answers a filter that throws with the envelope, not Boot's error body" {
            val response = get("/api/boom")

            response.statusCode() shouldBe 500
            response.body() shouldBe """{"code":"INTERNAL"}"""
            response.headers().firstValue("Content-Type").orElse("") shouldBe "application/json"
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
