package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.testsupport.AuthFixtures
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.loginBody
import io.kotest.assertions.throwables.shouldThrow
import io.kotest.assertions.withClue
import io.kotest.matchers.shouldBe
import jakarta.servlet.http.HttpServlet
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.http.MediaType
import org.springframework.mock.web.MockFilterChain
import org.springframework.mock.web.MockHttpServletRequest
import org.springframework.mock.web.MockHttpServletResponse
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post
import java.io.IOException

// #13 test 28: parity with Go's maxBodyBytes(1 << 20). Balances caps only its
// file uploads, which leaves an unbounded decode on every JSON route.
class MaxBodySpec : WebDatabaseSpec() {

    private val loginPath = AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN

    init {
        // requestBodyLargerThanOneMebibyteIsRejected
        "rejects a body larger than one mebibyte" {
            AuthFixtures.account(dataSource)

            val huge = """{"email":"${"a".repeat(2 shl 20)}@example.com","password":"x"}"""
            val result = mockMvc.perform(
                post(loginPath).contentType(MediaType.APPLICATION_JSON).content(huge),
            ).andReturn()

            result.response.status shouldBe 400
            result.response.contentAsString shouldBe """{"code":"INVALID_JSON_BODY"}"""
        }

        // The cap must not be the thing that answers an ordinary request.
        "leaves a normal body alone" {
            AuthFixtures.account(dataSource)

            mockMvc.perform(
                post(loginPath).contentType(MediaType.APPLICATION_JSON)
                    .content(loginBody("user@example.com", AuthFixtures.PASSWORD))
                    .with { it.remoteAddr = "198.51.100.60"; it },
            ).andReturn().response.status shouldBe 204
        }

        // Content-Length is absent on a chunked body, so the header check is
        // only half the cap — the stream itself has to count. Driven through
        // the filter directly because MockMvc always sets a content length.
        "counts the stream when no Content-Length is declared" {
            val request = object : MockHttpServletRequest("POST", "/api/auth/local/login") {
                // The whole point of the case: the filter must not be able to
                // short-circuit on a declared length.
                override fun getHeader(name: String): String? =
                    if (name.equals("Content-Length", ignoreCase = true)) null else super.getHeader(name)
            }
            request.setContent(ByteArray(2 shl 20))

            val chain = MockFilterChain(object : HttpServlet() {
                override fun service(req: HttpServletRequest, res: HttpServletResponse) {
                    req.inputStream.readAllBytes()
                }
            })

            shouldThrow<IOException> {
                MaxBodyFilter().doFilter(request, MockHttpServletResponse(), chain)
            }
        }

        // A body under the cap still has to arrive intact — the bulk read is
        // the path every JSON parse takes, so an off-by-one there would corrupt
        // ordinary requests rather than oversized ones.
        "passes a body under the cap through byte for byte" {
            val payload = ByteArray(64 * 1024) { (it % 251).toByte() }
            val request = MockHttpServletRequest("POST", "/api/auth/local/login")
            request.setContent(payload)

            var seen = ByteArray(0)
            val chain = MockFilterChain(object : HttpServlet() {
                override fun service(req: HttpServletRequest, res: HttpServletResponse) {
                    seen = req.inputStream.readAllBytes()
                }
            })

            MaxBodyFilter().doFilter(request, MockHttpServletResponse(), chain)

            seen.size shouldBe payload.size
            seen.contentEquals(payload) shouldBe true
        }
    }
}
