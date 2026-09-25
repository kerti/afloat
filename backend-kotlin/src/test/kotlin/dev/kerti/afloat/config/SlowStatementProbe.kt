package dev.kerti.afloat.config

import jakarta.persistence.EntityManager
import jakarta.persistence.PersistenceContext
import org.springframework.stereotype.Component
import org.springframework.transaction.annotation.Transactional

// A statement slow enough to exceed the 3s timeout configured for
// DatabaseTimeoutSpec. It has to run through JPA inside a Spring transaction,
// because that is where the transaction manager's default timeout is enforced.
@Component
class SlowStatementProbe {

    @PersistenceContext
    private lateinit var em: EntityManager

    @Transactional
    fun sleep(seconds: Int) {
        em.createNativeQuery("SELECT pg_sleep(:seconds)")
            .setParameter("seconds", seconds)
            .resultList
    }
}
