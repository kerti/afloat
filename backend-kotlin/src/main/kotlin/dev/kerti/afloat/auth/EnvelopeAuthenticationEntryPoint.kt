package dev.kerti.afloat.auth

import dev.kerti.afloat.api.model.ErrorCode
import dev.kerti.afloat.httperr.ApiErrorWriter
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.security.core.AuthenticationException
import org.springframework.security.web.AuthenticationEntryPoint

class EnvelopeAuthenticationEntryPoint : AuthenticationEntryPoint {
    override fun commence(request: HttpServletRequest, response: HttpServletResponse, authException: AuthenticationException) {
        ApiErrorWriter.write(response, 401, ErrorCode.UNAUTHORIZED)
    }
}
