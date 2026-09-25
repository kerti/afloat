package dev.kerti.afloat.httperr

import dev.kerti.afloat.api.model.ErrorCode
import dev.kerti.afloat.security.SecurityHeaders
import jakarta.servlet.RequestDispatcher
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.boot.webmvc.error.ErrorController
import org.springframework.stereotype.Controller
import org.springframework.web.bind.annotation.RequestMapping

// Replaces Boot's BasicErrorController, which answers the container's /error
// dispatch with {"timestamp","status","error","path"}: the one body in the app
// that was not ours. What reaches /error is what failed outside every handler:
//
// - a 5xx - a filter that threw, a response that failed to write - is 500
//   INTERNAL, which is what Go's recoverer answers (#32 item 1);
// - anything else is the bare status, no body, never Boot's.
//
// Either way with the fixed header set: OncePerRequestFilter skips the ERROR
// dispatch, so the security chain's HeaderWriterFilter never reaches this
// response, where Go's securityHeaders middleware reaches every one.
@Controller
class ApiErrorController : ErrorController {
    @RequestMapping("\${server.error.path:\${error.path:/error}}")
    fun error(request: HttpServletRequest, response: HttpServletResponse) {
        SecurityHeaders.write(request, response)
        // No error status means no error dispatch: a client asked for /error
        // itself. That is a path the API does not have, and answers as one,
        // as Go does for it.
        val status = request.getAttribute(RequestDispatcher.ERROR_STATUS_CODE) as? Int
        if (status == null) {
            response.status = HttpServletResponse.SC_NOT_FOUND
            return
        }
        if (status >= 500) {
            ApiErrorWriter.write(response, 500, ErrorCode.INTERNAL)
            return
        }
        response.status = status
    }
}
