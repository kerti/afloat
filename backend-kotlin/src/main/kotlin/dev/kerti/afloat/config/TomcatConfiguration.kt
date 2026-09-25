package dev.kerti.afloat.config

import org.apache.tomcat.util.buf.EncodedSolidusHandling
import org.springframework.boot.tomcat.TomcatConnectorCustomizer
import org.springframework.boot.tomcat.servlet.TomcatServletWebServerFactory
import org.springframework.boot.web.server.WebServerFactoryCustomizer
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration

@Configuration
class TomcatConfiguration {

    // Tomcat refuses an encoded slash (%2F) or backslash (%5C) itself, with a
    // 400 and its own HTML error page, before any filter runs. Passed through,
    // each reaches Spring Security's firewall, which refuses it the way it
    // refuses every other malformed path: as a path that does not exist, the
    // bare 404 an unregistered path gets (#56). Never decoded - that would make
    // either a separator, which the ruling forbids and Go's router does not do.
    //
    // A literal backslash, %00 and invalid UTF-8 are still Tomcat's 400: the
    // connector has no pass-through for them, and allowBackslash would turn a
    // backslash into a separator. #64.
    @Bean
    fun passEncodedSeparatorsToFirewall() = WebServerFactoryCustomizer<TomcatServletWebServerFactory> { factory ->
        factory.addConnectorCustomizers(TomcatConnectorCustomizer {
            it.encodedSolidusHandling = EncodedSolidusHandling.PASS_THROUGH.value
            it.encodedReverseSolidusHandling = EncodedSolidusHandling.PASS_THROUGH.value
        })
    }
}
