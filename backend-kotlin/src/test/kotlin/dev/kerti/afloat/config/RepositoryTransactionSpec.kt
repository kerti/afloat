package dev.kerti.afloat.config

import dev.kerti.afloat.testsupport.DatabaseSpec
import io.kotest.assertions.withClue
import io.kotest.matchers.booleans.shouldBeTrue
import io.kotest.matchers.collections.shouldNotBeEmpty
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.context.ApplicationContext
import org.springframework.core.annotation.AnnotatedElementUtils
import org.springframework.data.repository.support.Repositories
import org.springframework.transaction.annotation.Transactional

class RepositoryTransactionSpec : DatabaseSpec() {

    @Autowired
    private lateinit var applicationContext: ApplicationContext

    init {
        "every repository under dev.kerti.afloat carries Spring's @Transactional" {
            val repositories = Repositories(applicationContext)

            val repositoryInterfaces = repositories.map {
                repositories.getRequiredRepositoryInformation(it).repositoryInterface
            }.filter {
                it.packageName.startsWith("dev.kerti.afloat")
            }

            repositoryInterfaces.shouldNotBeEmpty()

            for (repositoryInterface in repositoryInterfaces) {
                withClue(repositoryInterface.name) {
                    AnnotatedElementUtils
                        .hasAnnotation(repositoryInterface, Transactional::class.java)
                        .shouldBeTrue()
                }
            }
        }
    }
}
