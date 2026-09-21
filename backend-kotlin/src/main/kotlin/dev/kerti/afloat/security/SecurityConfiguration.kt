package dev.kerti.afloat.security

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.api.SystemApi
import dev.kerti.afloat.auth.CrossSiteGuardFilter
import dev.kerti.afloat.auth.EnvelopeAccessDeniedHandler
import dev.kerti.afloat.auth.EnvelopeAuthenticationEntryPoint
import dev.kerti.afloat.auth.MaxBodyFilter
import dev.kerti.afloat.auth.RequestFactsFilter
import dev.kerti.afloat.auth.SessionCookieFactory
import dev.kerti.afloat.auth.SessionFilter
import dev.kerti.afloat.auth.data.SessionRepository
import dev.kerti.afloat.auth.data.UserRepository
import dev.kerti.afloat.config.AppConfig
import jakarta.servlet.DispatcherType
import org.springframework.beans.factory.annotation.Value
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import org.springframework.security.config.annotation.web.builders.HttpSecurity
import org.springframework.security.config.http.SessionCreationPolicy
import org.springframework.security.web.SecurityFilterChain
import org.springframework.security.web.authentication.UsernamePasswordAuthenticationFilter
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
    ): SecurityFilterChain {
        val maxBody = MaxBodyFilter()
        val crossSiteGuard = CrossSiteGuardFilter()
        val requestFacts = RequestFactsFilter()
        val sessionFilter = SessionFilter(sessionRepository, userRepository, sessionCookieFactory, clock, appConfig)
        return http
            .csrf { it.disable() }
            .formLogin { it.disable() }
            .httpBasic { it.disable() }
            .logout { it.disable() }
            .sessionManagement { it.sessionCreationPolicy(SessionCreationPolicy.STATELESS) }
            .authorizeHttpRequests {
                // An ERROR dispatch is authorized like any other since Spring
                // Security 6; without this an unauthenticated error forward
                // answers with the entry point's 401 instead of its own body.
                it.dispatcherTypeMatchers(DispatcherType.ERROR).permitAll()
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
            // order these lines happen to run in (server.go:75-86):
            // maxBody -> crossSiteGuard -> requestFacts -> session.
            .addFilterBefore(maxBody, UsernamePasswordAuthenticationFilter::class.java)
            .addFilterAfter(crossSiteGuard, MaxBodyFilter::class.java)
            .addFilterAfter(requestFacts, CrossSiteGuardFilter::class.java)
            .addFilterAfter(sessionFilter, RequestFactsFilter::class.java)
            .build()
    }
}
