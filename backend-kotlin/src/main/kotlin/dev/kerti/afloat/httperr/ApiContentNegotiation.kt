package dev.kerti.afloat.httperr

import org.springframework.context.annotation.Configuration
import org.springframework.http.MediaType
import org.springframework.web.servlet.config.annotation.ContentNegotiationConfigurer
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer

// The API speaks one media type, so the Accept header has nothing to choose
// between. Honoured, `Accept: text/html` on a JSON route is a 406 that Boot
// answers with its HTML error page — the one response in the app that is not
// an envelope. Go serves JSON whatever the client asked for; so does this.
@Configuration
class ApiContentNegotiation : WebMvcConfigurer {
    override fun configureContentNegotiation(configurer: ContentNegotiationConfigurer) {
        // With no strategy left the manager refuses to build, so JSON is named as
        // the default the request is served with.
        configurer.ignoreAcceptHeader(true).defaultContentType(MediaType.APPLICATION_JSON)
    }
}
