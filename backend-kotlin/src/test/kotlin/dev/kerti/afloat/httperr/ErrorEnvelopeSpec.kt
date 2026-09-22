package dev.kerti.afloat.httperr

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.api.SystemApi
import dev.kerti.afloat.api.model.ErrorCode
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import io.kotest.assertions.withClue
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldNotContain
import org.springframework.test.web.servlet.MvcResult
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.delete
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post

// #13 tests 22-24. Spring Boot's default error response is served from a path
// most exception handling never sees — the container's ERROR dispatch, not a
// controller — and it carries timestamp, status, error, path and sometimes
// message. Every one of those is a field the contract forbids, and `message`
// is the one that breaks non-negotiable 7 outright.
class ErrorEnvelopeSpec : WebDatabaseSpec() {

    // The keys Boot's DefaultErrorAttributes writes. None may appear anywhere.
    private val springDefaultKeys = listOf("timestamp", "\"status\"", "\"error\"", "\"path\"", "message", "trace")

    private fun MvcResult.carriesNoSpringDefault() {
        val body = response.contentAsString
        springDefaultKeys.forEach { key ->
            withClue("body was: $body") { body shouldNotContain key }
        }
    }

    init {
        // unknownRouteReturnsContractErrorEnvelope
        //
        // 404 for an anonymous caller too, as in Go, where RequireAuth wraps the
        // handlers and not the router (httpserver/server_test.go asserts it for
        // /api/nope). The chain is authenticated-by-default, but a path nothing
        // maps is let through to be a 404 rather than refused as a 401 that
        // claims the route exists. `/api/health/` is the case that showed it:
        // trailing slashes do not match, so it is unmapped.
        //
        // Bare, because the contract's ErrorCode enum has no NOT_FOUND. What is
        // in doubt for neither is the point of #13 test 22: never Boot's page.
        "answers an unknown route with a bare 404, never Boot's error body" {
            listOf("/api/nope", "/api/auth/nope", "/nope", "/api/health/").forEach { path ->
                val result = mockMvc.perform(get(path)).andReturn()
                withClue(path) {
                    result.response.status shouldBe 404
                    result.response.contentAsString shouldBe ""
                    result.carriesNoSpringDefault()
                }
            }
        }

        "keeps a mapped route behind authentication" {
            val result = mockMvc.perform(get(AuthApi.BASE_PATH + AuthApi.PATH_GET_ME)).andReturn()

            result.response.status shouldBe 401
        }

        // A JSON API has nothing to negotiate. Honoured, this was a 406 that
        // Boot answered with an HTML page and a Content-Language header.
        "ignores an Accept header that names no JSON" {
            val result = mockMvc.perform(
                get(AuthApi.BASE_PATH + AuthApi.PATH_GET_ME).header("Accept", "text/html"),
            ).andReturn()

            result.response.status shouldBe 401
            result.response.contentAsString shouldBe """{"code":"UNAUTHORIZED"}"""
            result.response.getHeader("Content-Language") shouldBe null
            result.carriesNoSpringDefault()
        }

        "answers a wrong method with a bare 405 and none of Boot's error body" {
            val result = mockMvc.perform(delete(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH)).andReturn()

            result.response.status shouldBe 405
            // RFC 9110 §15.5.6: a 405 must say what would have worked.
            result.response.getHeader("Allow") shouldBe "GET"
            result.carriesNoSpringDefault()
        }

        // errorEnvelopeCarriesNoMessageField
        //
        // Asserted on a response that really is an envelope, not only on the
        // empty ones above: the absence has to hold where there IS a body.
        "carries no message field on a real envelope" {
            val result = mockMvc.perform(get(AuthApi.BASE_PATH + AuthApi.PATH_GET_ME)).andReturn()

            result.response.status shouldBe 401
            result.response.contentAsString shouldBe """{"code":"UNAUTHORIZED"}"""
            result.carriesNoSpringDefault()
        }

        // The envelope written from a FILTER rather than a controller — the
        // path that has no @ControllerAdvice over it at all.
        "carries no message field on an envelope written by a filter" {
            val result = mockMvc.perform(
                post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN).header("Sec-Fetch-Site", "cross-site"),
            ).andReturn()

            result.response.status shouldBe 403
            result.response.contentAsString shouldBe """{"code":"CROSS_SITE_REQUEST_BLOCKED"}"""
            result.carriesNoSpringDefault()
        }

        // unhandledExceptionReturns500WithInternalCodeOnly
        //
        // Through the advice directly: an exception nothing anticipated cannot
        // be provoked through the wire without breaking a dependency, and the
        // guarantee being asserted is about the shape, not about the route.
        "reduces an unhandled exception to INTERNAL and nothing else" {
            val response = ApiExceptionHandler().handleUnexpected(
                IllegalStateException("connection to 10.0.0.4 refused for user afloat"),
            )

            response.statusCode.value() shouldBe 500
            response.body?.code shouldBe ErrorCode.INTERNAL
            // The cause is logged where it happened; the client is told nothing
            // about it. An args map here would leak the message the code exists
            // to replace.
            response.body?.args shouldBe null
        }
    }
}
