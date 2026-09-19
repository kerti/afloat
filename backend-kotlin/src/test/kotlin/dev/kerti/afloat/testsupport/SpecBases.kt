package dev.kerti.afloat.testsupport

import io.kotest.core.annotation.EnabledIf
import io.kotest.core.extensions.ApplyExtension
import io.kotest.core.spec.style.StringSpec
import io.kotest.extensions.spring.SpringExtension
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.context.SpringBootTest
import org.springframework.test.context.ContextConfiguration
import javax.sql.DataSource

// Any spec that needs a real container. No Docker => skipped, unless
// AFLOAT_REQUIRE_TEST_DB=1 (CI), in which case it stays enabled and fails loudly.
@EnabledIf(DockerOrRequired::class)
abstract class DockerGuardedSpec : StringSpec()

// A Spring context backed by the container. No initializer here: each concrete
// spec declares one, because Spring keys the context cache on the initializer
// classes, not on the database they point at.
@SpringBootTest
@ApplyExtension(SpringExtension::class)
abstract class SpringDatabaseSpec : DockerGuardedSpec() {

    @Autowired
    protected lateinit var dataSource: DataSource
}

// The default harness: the migrated shared database, truncated before every test
// so each test opens on a clean schema without a wrapping transaction.
@ContextConfiguration(initializers = [SharedDatabaseInitializer::class])
abstract class DatabaseSpec : SpringDatabaseSpec() {

    init {
        beforeTest { TestDatabase.truncate(dataSource) }
    }
}
