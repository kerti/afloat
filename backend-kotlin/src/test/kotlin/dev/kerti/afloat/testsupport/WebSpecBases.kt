package dev.kerti.afloat.testsupport

import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.webmvc.test.autoconfigure.AutoConfigureMockMvc
import org.springframework.test.web.servlet.MockMvc

// The default harness plus MockMvc, which drives the real Spring Security
// filter chain: the cross-site guard, the request-facts filter and the session
// filter all run, so a spec here asserts wired behaviour rather than a filter
// in isolation. The extra annotation gives it its own ApplicationContext, since
// Spring keys the context cache on the annotations rather than the class.
@AutoConfigureMockMvc
abstract class WebDatabaseSpec : DatabaseSpec() {

    @Autowired
    protected lateinit var mockMvc: MockMvc
}
