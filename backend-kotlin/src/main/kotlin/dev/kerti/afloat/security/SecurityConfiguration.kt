package dev.kerti.afloat.security

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.api.SystemApi
import dev.kerti.afloat.auth.CrossSiteGuardFilter
import dev.kerti.afloat.auth.DisabledLocalLogin404Filter
import dev.kerti.afloat.auth.EnvelopeAccessDeniedHandler
import dev.kerti.afloat.auth.EnvelopeAuthenticationEntryPoint
import dev.kerti.afloat.auth.MaxBodyFilter
import dev.kerti.afloat.auth.OptionsRefusedFilter
import dev.kerti.afloat.auth.RequestFactsFilter
import dev.kerti.afloat.auth.SessionCookieFactory
import dev.kerti.afloat.auth.SessionFilter
import dev.kerti.afloat.auth.data.SessionRepository
import dev.kerti.afloat.auth.data.UserRepository
import dev.kerti.afloat.config.AppConfig
import jakarta.servlet.DispatcherType
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.beans.factory.annotation.Value
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import org.springframework.security.config.annotation.web.builders.HttpSecurity
import org.springframework.security.config.http.SessionCreationPolicy
import org.springframework.security.web.SecurityFilterChain
import org.springframework.security.web.firewall.HttpFirewall
import org.springframework.security.web.firewall.RequestRejectedException
import org.springframework.security.web.firewall.RequestRejectedHandler
import org.springframework.security.web.firewall.StrictHttpFirewall
import org.springframework.security.web.authentication.UsernamePasswordAuthenticationFilter
import org.springframework.security.web.util.matcher.RequestMatcher
import org.springframework.web.servlet.HandlerMapping
import org.springframework.web.servlet.handler.AbstractHandlerMethodMapping
import java.time.Clock

@Configuration
class SecurityConfiguration {
    @Bean
    fun filterChain(
        http: HttpSecurity,
        @Value("\${api.base-path:/api}") basePath: String,
        sessionRepository: SessionRepository,
        userRepository: UserRepository,
        sessionCookieFactory: SessionCookieFactory,
        clock: Clock,
        appConfig: AppConfig,
        handlerMappings: List<HandlerMapping>,
    ): SecurityFilterChain {
        val optionsRefused = OptionsRefusedFilter()
        val localLogin404 = DisabledLocalLogin404Filter(appConfig.authLocalEnabled, basePath + AuthApi.PATH_LOCAL_LOGIN)
        val maxBody = MaxBodyFilter()
        val crossSiteGuard = CrossSiteGuardFilter()
        val requestFacts = RequestFactsFilter()
        val sessionFilter = SessionFilter(sessionRepository, userRepository, sessionCookieFactory, clock, appConfig)
        return http
            .csrf { it.disable() }
            .formLogin { it.disable() }
            .httpBasic { it.disable() }
            .logout { it.disable() }
            // SecurityHeaders' list, not Spring's defaults: the same list is
            // written where this chain's HeaderWriterFilter does not reach.
            .headers { header ->
                header.defaultsDisabled()
                SecurityHeaders.writers.forEach { header.addHeaderWriter(it) }
            }
            .sessionManagement { it.sessionCreationPolicy(SessionCreationPolicy.STATELESS) }
            .authorizeHttpRequests {
                // An ERROR dispatch is authorized like any other since Spring
                // Security 6; without this an unauthenticated error forward
                // answers with the entry point's 401 instead of its own body.
                it.dispatcherTypeMatchers(DispatcherType.ERROR).permitAll()
                // Requested directly, /error is a path the API does not have
                // (ApiErrorController answers the bare 404), and a missing path
                // is a 404 for everyone, not a 401 that says it exists.
                // server.error.path is left at Boot's default everywhere.
                it.requestMatchers("/error").permitAll()
                // Public by exception, authenticated by default: a new endpoint
                // in the contract cannot ship open because nobody remembered a
                // matcher (Go enforces the same per handler, server.go:82-86).
                // The paths come from the generated interfaces, so a rename in
                // the contract cannot silently reopen or close a route.
                it.requestMatchers(
                    basePath + SystemApi.PATH_GET_HEALTH,
                    basePath + SystemApi.PATH_GET_AUTH_METHODS,
                    basePath + AuthApi.PATH_LOCAL_LOGIN,
                    basePath + AuthApi.PATH_LOGOUT,
                ).permitAll()
                // A path no controller maps is a 404 for everyone, as in Go where
                // RequireAuth wraps handlers and not the router. Without this,
                // `/api/health/` (no such route: trailing slashes do not match)
                // answers an anonymous caller 401, which claims the route exists.
                // Every real endpoint has a mapping, so it still falls through to
                // authenticated() below; only the unmapped are let through to 404.
                it.requestMatchers(unmapped(handlerMappings)).permitAll()
                // Actuator is deliberately not public: it is not part of the
                // contract, and /api/health is the liveness route both
                // backends serve.
                it.anyRequest().authenticated()
            }
            // The envelope-writer entry point, so an unauthenticated /me is a
            // shaped 401, not the container's bare status. The access-denied
            // handler is wired with it rather than left at its default: both
            // write from a filter, outside controller exception handling, and
            // #14 §3.5 asks for both.
            .exceptionHandling {
                it.authenticationEntryPoint(EnvelopeAuthenticationEntryPoint())
                it.accessDeniedHandler(EnvelopeAccessDeniedHandler())
            }
            // Go's order, anchored explicitly rather than inherited from the
            // order these lines happen to run in (server.go:70-86):
            // optionsRefused -> localLogin404 -> maxBody -> crossSiteGuard -> requestFacts -> session.
            .addFilterBefore(maxBody, UsernamePasswordAuthenticationFilter::class.java)
            .addFilterBefore(localLogin404, MaxBodyFilter::class.java)
            .addFilterBefore(optionsRefused, DisabledLocalLogin404Filter::class.java)
            .addFilterAfter(crossSiteGuard, MaxBodyFilter::class.java)
            .addFilterAfter(requestFacts, CrossSiteGuardFilter::class.java)
            .addFilterAfter(sessionFilter, RequestFactsFilter::class.java)
            .build()
    }

    // The firewall Spring Security actually guards the chain with, replacing
    // the auto-configured StrictHttpFirewall with header VALUES accepted
    // (#67): a UTF-8 User-Agent or an em dash used to be a "header value with
    // a control character" refusal below, which Go never made and no client
    // here can avoid making, since Afloat's own denylist and email fields
    // already accept the same Unicode (BOOTSTRAP.md §5.1). Header NAMES stay
    // strict - only the value predicate changes. CR and LF are still refused,
    // by Tomcat at the connector, before this firewall ever sees the request.
    // Picked up automatically: WebSecurityConfiguration autowires the one
    // HttpFirewall bean in the context.
    @Bean
    fun httpFirewall(): HttpFirewall = StrictHttpFirewall().apply { setAllowedHeaderValues { true } }

    // A path StrictHttpFirewall refuses - `;`, `//`, `/./`, an encoded slash or
    // percent - is a path that does not exist (#56): a bare 404, as Go's router
    // gives it, not the firewall's 400 with Boot's error body. Written here
    // rather than through sendError: the refusal comes before the chain's
    // HeaderWriterFilter, and the /error dispatch sendError leads to skips it,
    // so the header set is written directly to answer exactly as an
    // unregistered path does. Found and used by WebSecurity as a bean.
    //
    // The firewall refuses more than paths - a method it does not know, and
    // (before #67) a header value with a control character - and those are a
    // malformed request on a route that exists, not a missing route. The
    // exception does not say which it was except in prose, so the request is
    // checked again by a firewall that looks at the path alone: refused
    // there, 404; otherwise a bare 400, the status these always had, with the
    // same header set.
    @Bean
    fun requestRejectedHandler() = RequestRejectedHandler { request, response, _ ->
        SecurityHeaders.write(request, response)
        response.status = when {
            // S1: OPTIONS on a firewall-refused path (`;x`, `//`, `%2e%2e`,
            // `%2F`) must answer the same flat 405 as every other OPTIONS
            // (#66) - OptionsRefusedFilter never gets a chance to run here,
            // since the firewall refuses the request before FilterChainProxy
            // ever dispatches into the filter chain, so this handler is the
            // one place left to make that hold. No Allow header, matching
            // optionsRefused (Go: httpserver/middleware.go).
            request.method == "OPTIONS" -> HttpServletResponse.SC_METHOD_NOT_ALLOWED
            pathRefused(request) -> HttpServletResponse.SC_NOT_FOUND
            else -> HttpServletResponse.SC_BAD_REQUEST
        }
    }

    private fun pathRefused(request: HttpServletRequest): Boolean = try {
        PATH_ONLY_FIREWALL.getFirewalledRequest(request)
        false
    } catch (e: RequestRejectedException) {
        true
    }

    private companion object {
        // StrictHttpFirewall's defaults for the path, and nothing else: any
        // method, any header, any parameter. getFirewalledRequest checks the
        // method, the URL and the host up front, and headers only as they are
        // read, so with the method check off it refuses on the URL alone.
        val PATH_ONLY_FIREWALL = StrictHttpFirewall().apply {
            setUnsafeAllowAnyHttpMethod(true)
            setAllowedHeaderNames { true }
            setAllowedHeaderValues { true }
            setAllowedParameterNames { true }
            setAllowedParameterValues { true }
        }
    }

    // True when no controller method maps the request. Only handler-method
    // mappings are asked — the controllers and the actuator's endpoints — not the
    // static-resource mapping, which claims every path and would make nothing
    // unmapped. A path that exists under another HTTP method makes getHandler
    // throw rather than return null, and that route is real: it stays
    // authenticated.
    private fun unmapped(handlerMappings: List<HandlerMapping>): RequestMatcher {
        val mappings = handlerMappings.filterIsInstance<AbstractHandlerMethodMapping<*>>()
        return RequestMatcher { request ->
            mappings.none { mapping ->
                try {
                    mapping.getHandler(request) != null
                } catch (e: Exception) {
                    true
                }
            }
        }
    }
}
