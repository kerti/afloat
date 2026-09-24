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
        "every auth.data repository carries Spring's @Transactional" {
            val repositories = Repositories(applicationContext)

            val authRepositories = repositories.map {
                repositories.getRequiredRepositoryInformation(it).repositoryInterface
            }.filter {
                it.packageName == "dev.kerti.afloat.auth.data"
            }

            authRepositories.shouldNotBeEmpty()

            for (repositoryInterface in authRepositories) {
                withClue(repositoryInterface.name) {
                    AnnotatedElementUtils
                        .hasAnnotation(repositoryInterface, Transactional::class.java)
                        .shouldBeTrue()
                }
            }
        }
    }
}
