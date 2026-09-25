package dev.kerti.afloat.httperr

import dev.kerti.afloat.api.model.ErrorCode
import jakarta.servlet.RequestDispatcher
import org.springframework.boot.web.error.ErrorAttributeOptions
import org.springframework.boot.webmvc.error.DefaultErrorAttributes
import org.springframework.stereotype.Component
import org.springframework.web.context.request.RequestAttributes
import org.springframework.web.context.request.WebRequest

// What Boot's /error writes when something fails past DispatcherServlet - a
// filter that throws, a response that fails to write - where neither
// ApiExceptionHandler nor a filter's own envelope reaches. Boot's default is
// {"timestamp","status","error","path"}, the one body in the app that is not
// the envelope (#32 item 1, CLAUDE.md rule 7). A 5xx becomes 500 INTERNAL,
// which is what Go's recoverer answers.
//
// Below 500 Boot's default stands for now. The only path that reaches /error
// with a 4xx today is the firewall's refusal of a malformed path, and what
// that should answer is #56's ruling to make, not this class's.
@Component
class EnvelopeErrorAttributes : DefaultErrorAttributes() {
    override fun getErrorAttributes(webRequest: WebRequest, options: ErrorAttributeOptions): MutableMap<String, Any?> {
        val status = webRequest.getAttribute(RequestDispatcher.ERROR_STATUS_CODE, RequestAttributes.SCOPE_REQUEST) as? Int
        if (status != null && status < 500) return super.getErrorAttributes(webRequest, options)
        return linkedMapOf("code" to ErrorCode.INTERNAL.value)
    }
}
