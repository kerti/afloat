package dev.kerti.afloat

import dev.kerti.afloat.testsupport.DockerGuardedSpec
import dev.kerti.afloat.testsupport.SharedDatabaseInitializer
import dev.kerti.afloat.testsupport.TestDatabase
import io.kotest.matchers.should
import io.kotest.matchers.shouldBe
import io.kotest.matchers.types.beTheSameInstanceAs
import org.springframework.boot.test.context.SpringBootTest
import org.springframework.test.context.ContextConfiguration
import org.springframework.test.context.TestContextManager

// Two real Spring test classes with identical configuration. They are not Kotest
// specs, so only the test below drives them: their point is that Spring's context
// cache serves one ApplicationContext, and therefore one DataSource and one
// container, for both. MergedContextConfiguration.equals ignores the test class,
// so identical config collapses to one cache entry.
@SpringBootTest
@ContextConfiguration(initializers = [SharedDatabaseInitializer::class])
class ReuseContextA

@SpringBootTest
@ContextConfiguration(initializers = [SharedDatabaseInitializer::class])
class ReuseContextB

class ContainerReuseSpec : DockerGuardedSpec() {

    init {
        "postgresContainerIsStartedOnceForTheWholeSuite" {
            val first = TestContextManager(ReuseContextA::class.java).testContext.applicationContext
            val second = TestContextManager(ReuseContextB::class.java).testContext.applicationContext

            first should beTheSameInstanceAs(second)
            TestDatabase.startCount shouldBe 1
        }
    }
}
