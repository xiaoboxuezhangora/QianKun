package com.example.entity;

import javax.persistence.Entity;
import javax.persistence.Id;

@Entity
public class Order {
    @Id
    private Long id;
    private String title;

    public Long getId() {
        return id;
    }
}
